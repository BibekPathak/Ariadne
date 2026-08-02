package store

import (
	"context"
	"time"
)

type Artifact struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	TaskID    string    `json:"task_id"`
	Type      string    `json:"type"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type ArtifactsRepo struct {
	pool pool
}

func (r *ArtifactsRepo) Create(ctx context.Context, a *Artifact) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO artifacts (id, agent_id, task_id, type, path, size) VALUES ($1,$2,$3,$4,$5,$6)`,
		a.ID, a.AgentID, a.TaskID, a.Type, a.Path, a.Size)
	return err
}

func (r *ArtifactsRepo) ListByAgent(ctx context.Context, agentID string) ([]*Artifact, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, task_id, type, path, size, created_at FROM artifacts WHERE agent_id=$1 ORDER BY created_at`,
		agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.AgentID, &a.TaskID, &a.Type, &a.Path, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (r *ArtifactsRepo) ListByTask(ctx context.Context, taskID string) ([]*Artifact, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, task_id, type, path, size, created_at FROM artifacts WHERE task_id=$1 ORDER BY created_at`,
		taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.AgentID, &a.TaskID, &a.Type, &a.Path, &a.Size, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
