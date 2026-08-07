package store

import (
	"context"
	"encoding/json"
	"time"
)

type Agent struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"org_id"`
	Template  string         `json:"template"`
	Goal      string         `json:"goal"`
	RepoURL   string         `json:"repo_url"`
	RepoPath  string         `json:"repo_path"`
	Status    string         `json:"status"`
	Error     string         `json:"error"`
	Meta      map[string]any `json:"meta"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type AgentsRepo struct {
	pool pool
}

func (r *AgentsRepo) Create(ctx context.Context, a *Agent) error {
	meta, err := json.Marshal(a.Meta)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO agents (id, org_id, template, goal, repo_url, repo_path, status, error, meta)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		a.ID, a.OrgID, a.Template, a.Goal, a.RepoURL, a.RepoPath, a.Status, a.Error, meta)
	return err
}

func (r *AgentsRepo) Get(ctx context.Context, id string) (*Agent, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, org_id, template, goal, repo_url, repo_path, status, error, meta, created_at, updated_at
		 FROM agents WHERE id=$1`, id)
	return scanAgent(row)
}

func (r *AgentsRepo) UpdateStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE agents SET status=$2, error=$3, updated_at=now() WHERE id=$1`,
		id, status, errMsg)
	return err
}

func (r *AgentsRepo) UpdateMeta(ctx context.Context, id string, meta map[string]any) error {
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE agents SET meta=$2, updated_at=now() WHERE id=$1`, id, raw)
	return err
}

func (r *AgentsRepo) List(ctx context.Context) ([]*Agent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, template, goal, repo_url, repo_path, status, error, meta, created_at, updated_at
		 FROM agents ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AgentsRepo) ListByOrg(ctx context.Context, orgID string) ([]*Agent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, template, goal, repo_url, repo_path, status, error, meta, created_at, updated_at
		 FROM agents WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AgentsRepo) CountByOrgStatuses(ctx context.Context, orgID string, statuses ...string) (int, error) {
	var n int
	rows, err := r.pool.Query(ctx,
		`SELECT COUNT(*) FROM agents WHERE org_id=$1 AND status = ANY($2::text[])`, orgID, statuses)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}

func (r *AgentsRepo) CountByOrgCreatedSince(ctx context.Context, orgID string, since time.Time) (int, error) {
	var n int
	rows, err := r.pool.Query(ctx,
		`SELECT COUNT(*) FROM agents WHERE org_id=$1 AND created_at >= $2`, orgID, since)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if rows.Next() {
		if err := rows.Scan(&n); err != nil {
			return 0, err
		}
	}
	return n, rows.Err()
}
