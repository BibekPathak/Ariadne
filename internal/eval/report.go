package eval

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"adriane/internal/store"
)

// LeaderboardRow is one arm's aggregate for a suite.
type LeaderboardRow struct {
	Arm          string  `json:"arm"`
	Suite        string  `json:"suite"`
	Tasks        int     `json:"tasks"`
	SuccessPct   float64 `json:"success_pct"`
	PassPct      float64 `json:"pass_pct"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	AvgCost      float64 `json:"avg_cost"`
	ToolErrors   int     `json:"tool_errors"`
	Tokens       int     `json:"tokens"`
}

// WriteArtifacts writes the reproducible eval outputs for one run.
func WriteArtifacts(dir string, run *store.EvalRun, results []*store.EvalResult, offline bool) (string, error) {
	dir = filepath.Join(dir, run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	summary := summaryFromMap(run.Summary)

	// leaderboard.json
	row := LeaderboardRow{
		Arm: run.Arm, Suite: run.Suite, Tasks: summary.Tasks,
		SuccessPct: summary.SuccessPct, PassPct: summary.PassPct,
		AvgLatencyMs: summary.AvgLatencyMs, AvgCost: summary.AvgCost,
		ToolErrors: summary.ToolErrors, Tokens: summary.Tokens,
	}
	if err := writeJSON(filepath.Join(dir, "leaderboard.json"), map[string]any{
		"run_id": run.ID, "git_sha": run.GitSHA, "suite_version": run.SuiteVersion,
		"row": row, "offline": offline,
	}); err != nil {
		return "", err
	}

	// results.json
	if err := writeJSON(filepath.Join(dir, "results.json"), results); err != nil {
		return "", err
	}

	// metrics.csv
	if err := writeCSV(filepath.Join(dir, "metrics.csv"), results); err != nil {
		return "", err
	}

	// summary.md
	note := ""
	if offline {
		note = "\n> Offline mode uses a shared mock provider; the leaderboard becomes meaningful when real `ROUTER_*_MODEL` IDs are configured.\n"
	}
	md := fmt.Sprintf("# Eval: %s / %s\n\nRun: %s  ·  git %s  ·  suite v%d%s\n\n| Metric | Value |\n|---|---|\n| Tasks | %d |\n| Success | %.1f%% |\n| Pass | %.1f%% |\n| Avg latency | %.0f ms |\n| Avg cost | $%.4f |\n| Tool errors | %d |\n| Tokens | %d |\n",
		run.Suite, run.Arm, run.ID, run.GitSHA, run.SuiteVersion, note,
		summary.Tasks, summary.SuccessPct, summary.PassPct,
		summary.AvgLatencyMs, summary.AvgCost, summary.ToolErrors, summary.Tokens)
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(md), 0o644); err != nil {
		return "", err
	}

	return dir, nil
}

func summaryFromMap(m map[string]any) Summary {
	return Summary{
		Tasks: intNum(m["tasks"]), Passed: intNum(m["passed"]), Succeeded: intNum(m["succeeded"]),
		SuccessPct: fNum(m["success_pct"]), PassPct: fNum(m["pass_pct"]),
		AvgLatencyMs: fNum(m["avg_latency_ms"]), AvgCost: fNum(m["avg_cost"]),
		ToolErrors: intNum(m["tool_errors"]), Tokens: intNum(m["tokens"]),
	}
}

func intNum(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func fNum(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func writeCSV(path string, results []*store.EvalResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"task", "agent_id", "success", "pass", "latency_ms", "cost", "tokens", "tool_errors", "error"}); err != nil {
		return err
	}
	for _, r := range results {
		if err := w.Write([]string{
			r.Task, r.AgentID, fmt.Sprintf("%v", r.Success), fmt.Sprintf("%v", r.Pass),
			fmt.Sprintf("%d", r.LatencyMs), fmt.Sprintf("%.6f", r.Cost),
			fmt.Sprintf("%d", r.Tokens), fmt.Sprintf("%d", r.ToolErrors), r.Error,
		}); err != nil {
			return err
		}
	}
	return nil
}
