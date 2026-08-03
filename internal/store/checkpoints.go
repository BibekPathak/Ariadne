package store

import (
	"context"
)

type CheckpointRow struct {
	TaskID    string
	AgentID   string
	Iteration int
	Messages  []byte // JSONB
}

// CheckpointRepo persists agent-loop state so a killed task can resume from
// its last tool boundary instead of restarting.
type CheckpointRepo struct {
	pool pool
}

func (r *CheckpointRepo) Upsert(ctx context.Context, taskID, agentID string, iteration int, messages []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO checkpoints (task_id, agent_id, iteration, messages, updated_at)
		 VALUES ($1,$2,$3,$4, now())
		 ON CONFLICT (task_id) DO UPDATE SET
		   agent_id=EXCLUDED.agent_id, iteration=EXCLUDED.iteration,
		   messages=EXCLUDED.messages, updated_at=now()`,
		taskID, agentID, iteration, messages)
	return err
}

func (r *CheckpointRepo) Get(ctx context.Context, taskID string) (*CheckpointRow, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT task_id, agent_id, iteration, messages FROM checkpoints WHERE task_id=$1`, taskID)
	var c CheckpointRow
	err := row.Scan(&c.TaskID, &c.AgentID, &c.Iteration, &c.Messages)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CheckpointRepo) Delete(ctx context.Context, taskID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM checkpoints WHERE task_id=$1`, taskID)
	return err
}
