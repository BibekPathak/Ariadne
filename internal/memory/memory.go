package memory

import (
	"context"
	"time"
)

// Kind values describe what a memory represents.
const (
	KindConversation = "conversation"
	KindPlan         = "plan"
	KindFiles        = "files"
	KindPreference   = "preference"
	KindOutcome      = "outcome"
	KindFailure      = "failure"
	KindArtifact     = "artifact"
)

// Entry is a single memory item across all three tiers.
type Entry struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Kind      string    `json:"kind"`
	Topic     string    `json:"topic"`
	Content   string    `json:"content"`
	Score     float32   `json:"score,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// IndexItem is a piece of content to embed and index semantically.
type IndexItem struct {
	ID      string
	AgentID string
	Kind    string
	Topic   string
	Content string
	Vector  []float32
}

// ShortTermStore holds ephemeral, per-agent state (Redis).
type ShortTermStore interface {
	StoreShortTerm(ctx context.Context, agentID string, entries []Entry) error
	LoadShortTerm(ctx context.Context, agentID string, limit int) ([]Entry, error)
}

// LongTermStore holds durable, queryable memories (Postgres).
type LongTermStore interface {
	StoreLongTerm(ctx context.Context, agentID string, e Entry) error
	LoadLongTerm(ctx context.Context, agentID string, topics []string, limit int) ([]Entry, error)
}

// SemanticIndex is the low-level vector backend (native store now, Qdrant
// later). It deals in vectors; the manager owns embedding.
type SemanticIndex interface {
	Index(ctx context.Context, items []IndexItem) error
	Search(ctx context.Context, agentID string, query []float32, limit int) ([]Entry, error)
}

// SemanticStore is the high-level semantic memory capability: it hides
// embedding from callers.
type SemanticStore interface {
	IndexSemantic(ctx context.Context, agentID string, items []IndexItem) error
	SearchSemantic(ctx context.Context, agentID string, query string, limit int) ([]Entry, error)
}

// Memory is the unified, swappable seam the rest of the platform uses.
type Memory interface {
	ShortTermStore
	LongTermStore
	SemanticStore
	Close() error
}
