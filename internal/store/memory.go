package store

import (
	"context"
	"time"
)

type MemoryRow struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Kind      string    `json:"kind"`
	Topic     string    `json:"topic"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// MemoryRepo persists long-term memories. Short-term (Redis) and semantic
// (vectors) memories live outside Postgres.
type MemoryRepo struct {
	pool pool
}

func (r *MemoryRepo) Create(ctx context.Context, m *MemoryRow) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO memories (id, agent_id, kind, topic, content) VALUES ($1,$2,$3,$4,$5)`,
		m.ID, m.AgentID, m.Kind, m.Topic, m.Content)
	return err
}

func (r *MemoryRepo) ListByTopics(ctx context.Context, agentID string, topics []string, limit int) ([]*MemoryRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, kind, topic, content, created_at FROM memories
		 WHERE agent_id=$1 AND (
		     topic = ANY($2::text[])
		     OR EXISTS (SELECT 1 FROM unnest($2::text[]) w WHERE topic ILIKE '%' || w || '%')
		 )
		 ORDER BY created_at DESC LIMIT $3`,
		agentID, topics, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

func (r *MemoryRepo) ListRecent(ctx context.Context, agentID string, limit int) ([]*MemoryRow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, agent_id, kind, topic, content, created_at FROM memories
		 WHERE agent_id=$1 ORDER BY created_at DESC LIMIT $2`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

func scanMemoryRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]*MemoryRow, error) {
	var out []*MemoryRow
	for rows.Next() {
		var m MemoryRow
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Kind, &m.Topic, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}
