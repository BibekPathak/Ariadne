package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type Task struct {
	ID         string         `json:"id"`
	AgentID    string         `json:"agent_id"`
	Name       string         `json:"name"`
	Template   string         `json:"template"`
	Status     string         `json:"status"`
	DependsOn  []string       `json:"depends_on"`
	Inputs     map[string]any `json:"inputs"`
	Outputs    map[string]any `json:"outputs"`
	Attempt    int            `json:"attempt"`
	MaxAttempt int            `json:"max_attempt"`
	Error      string         `json:"error"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

type TasksRepo struct {
	pool pool
}

func (r *TasksRepo) Create(ctx context.Context, t *Task) error {
	inputs, err := json.Marshal(t.Inputs)
	if err != nil {
		return err
	}
	outputs, err := json.Marshal(t.Outputs)
	if err != nil {
		return err
	}
	depends := arrayLiteral(t.DependsOn)
	_, err = r.pool.Exec(ctx,
		`INSERT INTO tasks (id, agent_id, name, template, status, depends_on, inputs, outputs, attempt, max_attempt, error)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		t.ID, t.AgentID, t.Name, t.Template, t.Status, depends, inputs, outputs, t.Attempt, t.MaxAttempt, t.Error)
	return err
}

// arrayLiteral renders a []string as a Postgres array literal.
func arrayLiteral(in []string) string {
	if len(in) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range in {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(s))
	}
	b.WriteByte('}')
	return b.String()
}

func (r *TasksRepo) Get(ctx context.Context, id string) (*Task, error) {
	return scanTask(r.pool.QueryRow(ctx,
		`SELECT id, agent_id, name, template, status, depends_on, inputs, outputs, attempt, max_attempt, error, created_at, updated_at
		 FROM tasks WHERE id=$1`, id))
}

func (r *TasksRepo) ListByAgent(ctx context.Context, agentID string) ([]*Task, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, name, template, status, depends_on, inputs, outputs, attempt, max_attempt, error, created_at, updated_at
		 FROM tasks WHERE agent_id=$1 ORDER BY created_at`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TasksRepo) UpdateStatus(ctx context.Context, id, status, errMsg string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE tasks SET status=$2, error=$3, updated_at=now() WHERE id=$1`, id, status, errMsg)
	return err
}

func (r *TasksRepo) UpdateOutputs(ctx context.Context, id string, outputs map[string]any) error {
	raw, err := json.Marshal(outputs)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE tasks SET outputs=$2, updated_at=now() WHERE id=$1`, id, raw)
	return err
}

func (r *TasksRepo) IncAttempt(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE tasks SET attempt=attempt+1, updated_at=now() WHERE id=$1`, id)
	return err
}
