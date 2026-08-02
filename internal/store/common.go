package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pool is satisfied by *pgxpool.Pool and pgx.Tx, so repositories work in
// either a standalone or transactional context.
type pool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanAgent(row pgx.Row) (*Agent, error) {
	var a Agent
	var meta []byte
	err := row.Scan(&a.ID, &a.Template, &a.Goal, &a.RepoURL, &a.RepoPath,
		&a.Status, &a.Error, &meta, &a.CreatedAt, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	a.Meta = map[string]any{}
	_ = json.Unmarshal(meta, &a.Meta)
	return &a, nil
}

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	var inputs, outputs []byte
	var depends []string
	err := row.Scan(&t.ID, &t.AgentID, &t.Name, &t.Template, &t.Status, &depends,
		&inputs, &outputs, &t.Attempt, &t.MaxAttempt, &t.Error, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.DependsOn = depends
	t.Inputs = map[string]any{}
	t.Outputs = map[string]any{}
	_ = json.Unmarshal(inputs, &t.Inputs)
	_ = json.Unmarshal(outputs, &t.Outputs)
	return &t, nil
}
