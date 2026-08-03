package checkpoint

import (
	"context"
	"sync"

	"adriane/internal/llm"
)

// InMemory is a test-friendly Store. The real path uses Postgres.
type InMemory struct {
	mu     sync.Mutex
	states map[string]*Checkpoint
}

func NewInMemory() *InMemory { return &InMemory{states: map[string]*Checkpoint{}} }

func (m *InMemory) Save(ctx context.Context, cp *Checkpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *cp
	clone.Messages = append([]llm.Message(nil), cp.Messages...)
	m.states[cp.TaskID] = &clone
	return nil
}

func (m *InMemory) Load(ctx context.Context, taskID string) (*Checkpoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp, ok := m.states[taskID]
	if !ok {
		return nil, nil
	}
	clone := *cp
	clone.Messages = append([]llm.Message(nil), cp.Messages...)
	return &clone, nil
}

func (m *InMemory) Delete(ctx context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, taskID)
	return nil
}
