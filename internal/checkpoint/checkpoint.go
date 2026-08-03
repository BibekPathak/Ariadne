// Package checkpoint persists agent-loop state at tool boundaries so a task
// killed mid-run can resume from its last checkpoint instead of restarting.
package checkpoint

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"adriane/internal/llm"
	"adriane/internal/store"
)

// Checkpoint is the durable agent-loop state for one task.
type Checkpoint struct {
	AgentID   string
	TaskID    string
	Iteration int
	Messages  []llm.Message
}

// Store is the seam the worker checkpoints through.
type Store interface {
	Save(ctx context.Context, cp *Checkpoint) error
	Load(ctx context.Context, taskID string) (*Checkpoint, error)
	Delete(ctx context.Context, taskID string) error
}

// Postgres persists checkpoints in the shared database so any worker can
// resume a task after its original executor died.
type Postgres struct {
	repo *store.CheckpointRepo
}

func NewPostgres(repo *store.CheckpointRepo) *Postgres { return &Postgres{repo: repo} }

func (p *Postgres) Save(ctx context.Context, cp *Checkpoint) error {
	raw, err := json.Marshal(cp.Messages)
	if err != nil {
		return err
	}
	return p.repo.Upsert(ctx, cp.TaskID, cp.AgentID, cp.Iteration, raw)
}

func (p *Postgres) Load(ctx context.Context, taskID string) (*Checkpoint, error) {
	row, err := p.repo.Get(ctx, taskID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	cp := &Checkpoint{AgentID: row.AgentID, TaskID: row.TaskID, Iteration: row.Iteration}
	if err := json.Unmarshal(row.Messages, &cp.Messages); err != nil {
		return nil, err
	}
	return cp, nil
}

func (p *Postgres) Delete(ctx context.Context, taskID string) error {
	return p.repo.Delete(ctx, taskID)
}
