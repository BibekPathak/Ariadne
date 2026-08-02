package memory

import (
	"context"
	"fmt"
	"log/slog"

	"adriane/internal/llm"
)

// Manager composes the three memory tiers behind the Memory interface and
// owns embedding, so callers never touch vectors directly.
type Manager struct {
	shortTerm ShortTermStore
	longTerm  LongTermStore
	semantic  SemanticIndex
	embed     llm.Provider
	logger    *slog.Logger
}

type ManagerConfig struct {
	ShortTerm ShortTermStore
	LongTerm  LongTermStore
	Semantic  SemanticIndex
	Embedder  llm.Provider
	Logger    *slog.Logger
}

func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.ShortTerm == nil || cfg.LongTerm == nil {
		return nil, fmt.Errorf("memory: short-term and long-term stores are required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		shortTerm: cfg.ShortTerm,
		longTerm:  cfg.LongTerm,
		semantic:  cfg.Semantic,
		embed:     cfg.Embedder,
		logger:    cfg.Logger,
	}, nil
}

func (m *Manager) StoreShortTerm(ctx context.Context, agentID string, entries []Entry) error {
	return m.shortTerm.StoreShortTerm(ctx, agentID, entries)
}

func (m *Manager) LoadShortTerm(ctx context.Context, agentID string, limit int) ([]Entry, error) {
	return m.shortTerm.LoadShortTerm(ctx, agentID, limit)
}

func (m *Manager) StoreLongTerm(ctx context.Context, agentID string, e Entry) error {
	return m.longTerm.StoreLongTerm(ctx, agentID, e)
}

func (m *Manager) LoadLongTerm(ctx context.Context, agentID string, topics []string, limit int) ([]Entry, error) {
	return m.longTerm.LoadLongTerm(ctx, agentID, topics, limit)
}

// IndexSemantic embeds the given items and indexes them. Returns a no-op when
// no embedder or semantic store is configured.
func (m *Manager) IndexSemantic(ctx context.Context, agentID string, items []IndexItem) error {
	if m.semantic == nil || m.embed == nil || len(items) == 0 {
		return nil
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Content
	}
	vectors, err := m.embed.Embed(ctx, texts)
	if err != nil {
		m.logger.Warn("semantic indexing skipped", "err", err)
		return nil
	}
	for i := range items {
		items[i].AgentID = agentID
		items[i].Vector = vectors[i]
	}
	return m.semantic.Index(ctx, items)
}

func (m *Manager) SearchSemantic(ctx context.Context, agentID string, query string, limit int) ([]Entry, error) {
	if m.semantic == nil || m.embed == nil {
		return nil, nil
	}
	q, err := m.embed.Embed(ctx, []string{query})
	if err != nil || len(q) == 0 {
		m.logger.Warn("semantic search skipped", "err", err)
		return nil, nil
	}
	return m.semantic.Search(ctx, agentID, q[0], limit)
}

func (m *Manager) Close() error {
	if m.shortTerm != nil {
		if c, ok := m.shortTerm.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	if m.semantic != nil {
		if c, ok := m.semantic.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	return nil
}
