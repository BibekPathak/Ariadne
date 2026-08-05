package store

import (
	"context"
	"encoding/json"
	"time"
)

type EvalRun struct {
	ID           string         `json:"id"`
	Suite        string         `json:"suite"`
	SuiteVersion int            `json:"suite_version"`
	Arm          string         `json:"arm"`
	GitSHA       string         `json:"git_sha"`
	Status       string         `json:"status"`
	Summary      map[string]any `json:"summary"`
	CreatedAt    time.Time      `json:"created_at"`
	CompletedAt  *time.Time     `json:"completed_at"`
}

type EvalResult struct {
	RunID      string         `json:"run_id"`
	Task       string         `json:"task"`
	AgentID    string         `json:"agent_id"`
	Success    bool           `json:"success"`
	Pass       bool           `json:"pass"`
	LatencyMs  int64          `json:"latency_ms"`
	Cost       float64        `json:"cost"`
	Tokens     int            `json:"tokens"`
	ToolErrors int            `json:"tool_errors"`
	Checks     map[string]any `json:"checks"`
	Error      string         `json:"error"`
}

type EvalRunsRepo struct{ pool pool }
type EvalResultsRepo struct{ pool pool }

func (r *EvalRunsRepo) Create(ctx context.Context, e *EvalRun) error {
	sum, err := json.Marshal(e.Summary)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO eval_runs (id, suite, suite_version, arm, git_sha, status, summary)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ID, e.Suite, e.SuiteVersion, e.Arm, e.GitSHA, e.Status, sum)
	return err
}

func (r *EvalRunsRepo) UpdateSummary(ctx context.Context, id string, status string, summary map[string]any) error {
	sum, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE eval_runs SET status=$2, summary=$3, completed_at=now() WHERE id=$1`, id, status, sum)
	return err
}

// Latest returns the most recent completed run for a suite+arm, or nil.
func (r *EvalRunsRepo) Latest(ctx context.Context, suite, arm string) (*EvalRun, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, suite, suite_version, arm, git_sha, status, summary, created_at, completed_at
		 FROM eval_runs WHERE suite=$1 AND arm=$2 AND status='completed'
		 ORDER BY created_at DESC LIMIT 1`, suite, arm)
	return scanEvalRun(row)
}

func scanEvalRun(row interface{ Scan(...any) error }) (*EvalRun, error) {
	var e EvalRun
	var summary []byte
	var completed *time.Time
	err := row.Scan(&e.ID, &e.Suite, &e.SuiteVersion, &e.Arm, &e.GitSHA, &e.Status, &summary, &e.CreatedAt, &completed)
	if err != nil {
		return nil, err
	}
	e.CompletedAt = completed
	e.Summary = map[string]any{}
	_ = json.Unmarshal(summary, &e.Summary)
	return &e, nil
}

func (r *EvalResultsRepo) Create(ctx context.Context, res *EvalResult) error {
	checks, err := json.Marshal(res.Checks)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO eval_results (run_id, task, agent_id, success, pass, latency_ms, cost, tokens, tool_errors, checks, error)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		res.RunID, res.Task, res.AgentID, res.Success, res.Pass, res.LatencyMs, res.Cost, res.Tokens, res.ToolErrors, checks, res.Error)
	return err
}

func (r *EvalResultsRepo) ListByRun(ctx context.Context, runID string) ([]*EvalResult, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT run_id, task, agent_id, success, pass, latency_ms, cost, tokens, tool_errors, checks, error
		 FROM eval_results WHERE run_id=$1 ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EvalResult
	for rows.Next() {
		var res EvalResult
		var checks []byte
		if err := rows.Scan(&res.RunID, &res.Task, &res.AgentID, &res.Success, &res.Pass,
			&res.LatencyMs, &res.Cost, &res.Tokens, &res.ToolErrors, &checks, &res.Error); err != nil {
			return nil, err
		}
		res.Checks = map[string]any{}
		_ = json.Unmarshal(checks, &res.Checks)
		out = append(out, &res)
	}
	return out, rows.Err()
}
