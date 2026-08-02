package events

import (
	"context"
	"log/slog"
	"sync"
)

// PersistFn appends an event to durable storage and returns the sequence
// number assigned to it (the DB owns the counter via BIGSERIAL).
type PersistFn func(e Event) (int64, error)

// InMemoryBus fans events out to subscribers and, if a Persist hook is set,
// writes every event to durable storage before broadcasting. That makes it
// both the transport seam (NATS later) and the event-sourcing sink.
type InMemoryBus struct {
	mu      sync.RWMutex
	subs    map[chan Event]struct{}
	persist PersistFn
	logger  *slog.Logger
}

func NewInMemoryBus(logger *slog.Logger) *InMemoryBus {
	if logger == nil {
		logger = slog.Default()
	}
	return &InMemoryBus{subs: make(map[chan Event]struct{}), logger: logger}
}

// SetPersister registers an append-only sink. Events are persisted before
// fan-out so subscribers always observe a durable, ordered stream.
func (b *InMemoryBus) SetPersister(fn PersistFn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.persist = fn
}

func (b *InMemoryBus) Publish(ctx context.Context, e Event) error {
	b.mu.Lock()
	if b.persist != nil {
		seq, err := b.persist(e)
		if err != nil {
			b.mu.Unlock()
			return err
		}
		e.Seq = seq
	}
	subs := make([]chan Event, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (b *InMemoryBus) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
	return ch, cancel
}

func (b *InMemoryBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		close(ch)
		delete(b.subs, ch)
	}
	return nil
}
