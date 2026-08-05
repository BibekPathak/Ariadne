package eval

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"adriane/internal/agents"
	"adriane/internal/events"
	"adriane/internal/store"
)

// RunnerService is the subset of the control plane the eval runner needs.
type RunnerService interface {
	Create(ctx context.Context, req agents.CreateRequest) (*store.Agent, error)
	Run(ctx context.Context, agentID string) error
	Events(ctx context.Context, agentID string) ([]events.Event, error)
}

type Runner struct {
	svc     RunnerService
	repos   *store.EvalRunsRepo
	results *store.EvalResultsRepo
	pricing Pricing
	tier    string // arm: fast | coding | reasoning
	workdir string // scratch repos
}

func NewRunner(svc RunnerService, repos *store.EvalRunsRepo, results *store.EvalResultsRepo, pricing Pricing, tier, workdir string) *Runner {
	if workdir == "" {
		workdir = filepath.Join(os.TempDir(), "kubeai-eval")
	}
	return &Runner{svc: svc, repos: repos, results: results, pricing: pricing, tier: tier, workdir: workdir}
}

// RunSuite executes every task in the suite under the runner's tier and
// persists a completed eval run.
func (r *Runner) RunSuite(ctx context.Context, suite *Suite) (*store.EvalRun, error) {
	run := &store.EvalRun{
		ID:           "eval-" + time.Now().UTC().Format("20060102T150405Z"),
		Suite:        suite.Name,
		SuiteVersion: suite.Version,
		Arm:          r.tier,
		GitSHA:       gitSHA(),
		Status:       "running",
		Summary:      map[string]any{},
	}
	if err := r.repos.Create(ctx, run); err != nil {
		return nil, err
	}

	var results []*store.EvalResult
	for _, task := range suite.Tasks {
		res, err := r.runTask(ctx, run.ID, task)
		if err != nil {
			return nil, err
		}
		if err := r.results.Create(ctx, res); err != nil {
			return nil, err
		}
		results = append(results, res)
	}

	summary := summarize(results)
	if err := r.repos.UpdateSummary(ctx, run.ID, "completed", toMap(summary)); err != nil {
		return nil, err
	}
	run.Status = "completed"
	run.Summary = toMap(summary)
	return run, nil
}

func (r *Runner) runTask(ctx context.Context, runID string, task Task) (*store.EvalResult, error) {
	scratch := filepath.Join(r.workdir, runID, task.Name)
	_ = os.RemoveAll(scratch)
	if err := copyTree(task.Repo, scratch); err != nil {
		return nil, fmt.Errorf("stage repo for %s: %w", task.Name, err)
	}
	defer os.RemoveAll(scratch)

	agent, err := r.svc.Create(ctx, agents.CreateRequest{
		Template: task.Template, Goal: task.Goal, RepoPath: scratch, Tier: r.tier,
	})
	if err != nil {
		return nil, err
	}
	runErr := r.svc.Run(ctx, agent.ID)

	evs, _ := r.svc.Events(ctx, agent.ID)
	metrics := MetricsFromEvents(evs, r.pricing.PricePer1k(r.tier))

	verdict, _ := (CompositeJudge{Expected: task.Expected, WorkDir: scratch}).Score(ctx, Outcome{
		Task: task.Name, AgentID: agent.ID, Success: runErr == nil, RepoDir: scratch,
		LatencyMs: metrics.LatencyMs, Cost: metrics.Cost, Tokens: metrics.Tokens, ToolErrors: metrics.ToolErrors,
		Error: errString(runErr),
	})

	checks := map[string]any{"pass": verdict.Pass, "reason": verdict.Reason}
	return &store.EvalResult{
		RunID: runID, Task: task.Name, AgentID: agent.ID,
		Success: runErr == nil, Pass: verdict.Pass,
		LatencyMs: metrics.LatencyMs, Cost: metrics.Cost, Tokens: metrics.Tokens, ToolErrors: metrics.ToolErrors,
		Checks: checks, Error: errString(runErr),
	}, nil
}

// Summary aggregates one eval run. The headline metrics are rubric-based:
// success reflects the judge verdict (agent completion is a precondition of
// must_succeed checks), not mere task completion.
type Summary struct {
	Tasks        int     `json:"tasks"`
	Passed       int     `json:"passed"`
	Succeeded    int     `json:"succeeded"`
	SuccessPct   float64 `json:"success_pct"`
	PassPct      float64 `json:"pass_pct"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	AvgCost      float64 `json:"avg_cost"`
	ToolErrors   int     `json:"tool_errors"`
	Tokens       int     `json:"tokens"`
}

func summarize(results []*store.EvalResult) Summary {
	s := Summary{Tasks: len(results)}
	for _, res := range results {
		if res.Pass {
			s.Passed++
			s.Succeeded++
		}
		s.AvgLatencyMs += float64(res.LatencyMs)
		s.AvgCost += res.Cost
		s.ToolErrors += res.ToolErrors
		s.Tokens += res.Tokens
	}
	if s.Tasks > 0 {
		s.SuccessPct = 100 * float64(s.Succeeded) / float64(s.Tasks)
		s.PassPct = 100 * float64(s.Passed) / float64(s.Tasks)
		s.AvgLatencyMs /= float64(s.Tasks)
		s.AvgCost /= float64(s.Tasks)
	}
	return s
}

func toMap(s Summary) map[string]any {
	// Store numeric values as float64 so they survive the JSON round-trip
	// through Postgres uniformly.
	return map[string]any{
		"tasks": float64(s.Tasks), "passed": float64(s.Passed), "succeeded": float64(s.Succeeded),
		"success_pct": s.SuccessPct, "pass_pct": s.PassPct,
		"avg_latency_ms": s.AvgLatencyMs, "avg_cost": s.AvgCost,
		"tool_errors": float64(s.ToolErrors), "tokens": float64(s.Tokens),
	}
}

func gitSHA() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1])
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}
