package store

import (
	"context"
	"encoding/json"
	"time"

	"kubeai/internal/events"
)

type EventsRepo struct {
	pool pool
}

func (r *EventsRepo) Append(ctx context.Context, e events.Event) (int64, error) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = r.pool.QueryRow(ctx,
		`INSERT INTO events (agent_id, task_id, type, payload, ts) VALUES ($1,$2,$3,$4,$5) RETURNING seq`,
		e.AgentID, e.TaskID, string(e.Type), payload, e.TS).Scan(&seq)
	return seq, err
}

func (r *EventsRepo) ListByAgent(ctx context.Context, agentID string) ([]events.Event, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT seq, agent_id, task_id, type, payload, ts FROM events WHERE agent_id=$1 ORDER BY seq`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (r *EventsRepo) StreamAfterSeq(ctx context.Context, agentID string, afterSeq int64) ([]events.Event, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT seq, agent_id, task_id, type, payload, ts FROM events WHERE agent_id=$1 AND seq>$2 ORDER BY seq`,
		agentID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]events.Event, error) {
	var out []events.Event
	for rows.Next() {
		var e events.Event
		var t string
		var payload []byte
		var ts time.Time
		if err := rows.Scan(&e.Seq, &e.AgentID, &e.TaskID, &t, &payload, &ts); err != nil {
			return nil, err
		}
		e.Type = events.Type(t)
		e.Payload = map[string]any{}
		_ = json.Unmarshal(payload, &e.Payload)
		e.TS = ts
		out = append(out, e)
	}
	return out, rows.Err()
}
