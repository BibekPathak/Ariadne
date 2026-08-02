package memory

import (
	"context"
	"time"

	"adriane/internal/store"
)

// LongTermPostgres persists durable memories in Postgres.
type LongTermPostgres struct {
	repo *store.MemoryRepo
}

func NewLongTermPostgres(repo *store.MemoryRepo) *LongTermPostgres {
	return &LongTermPostgres{repo: repo}
}

func (l *LongTermPostgres) StoreLongTerm(ctx context.Context, agentID string, e Entry) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	return l.repo.Create(ctx, &store.MemoryRow{
		ID: e.ID, AgentID: agentID, Kind: e.Kind, Topic: e.Topic, Content: e.Content, CreatedAt: e.CreatedAt,
	})
}

func (l *LongTermPostgres) LoadLongTerm(ctx context.Context, agentID string, topics []string, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []*store.MemoryRow
	var err error
	if len(topics) == 0 {
		rows, err = l.repo.ListRecent(ctx, agentID, limit)
	} else {
		rows, err = l.repo.ListByTopics(ctx, agentID, topics, limit)
	}
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{
			ID: r.ID, AgentID: r.AgentID, Kind: r.Kind, Topic: r.Topic,
			Content: r.Content, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}
