package workflow

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"adriane/internal/events"
	"adriane/internal/store"
)

// memStore is an in-memory NodeStore for engine tests.
type memStore struct {
	mu    sync.Mutex
	tasks map[string]*store.Task
}

func newMemStore() *memStore { return &memStore{tasks: map[string]*store.Task{}} }

func (m *memStore) Create(ctx context.Context, t *store.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[t.ID] = t
	return nil
}
func (m *memStore) UpdateStatus(ctx context.Context, id, status, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Status = status
		t.Error = errMsg
	}
	return nil
}
func (m *memStore) UpdateOutputs(ctx context.Context, id string, outputs map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[id]; ok {
		t.Outputs = outputs
	}
	return nil
}
func (m *memStore) IncAttempt(ctx context.Context, id string) error { return nil }
func (m *memStore) status(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[id].Status
}

// barrierExecutor blocks until both goroutines are inside Execute, proving the
// engine really runs ready nodes concurrently.
type barrierExecutor struct {
	entered chan struct{}
	release chan struct{}
	hits    atomic.Int32
}

func newBarrierExecutor() *barrierExecutor {
	return &barrierExecutor{entered: make(chan struct{}, 2), release: make(chan struct{})}
}

func (b *barrierExecutor) Execute(ctx context.Context, node *Node) (map[string]any, error) {
	b.hits.Add(1)
	b.entered <- struct{}{}
	<-b.release
	return map[string]any{"node": node.Name}, nil
}

func TestEngineRunsIndependentNodesConcurrently(t *testing.T) {
	ns := newMemStore()
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	e := NewEngine(ns, bus, slog.New(slog.DiscardHandler), 2, nil)

	exec := newBarrierExecutor()
	dag := &DAG{AgentID: "a", Nodes: []*Node{
		{ID: "a_1", AgentID: "a", Name: "left", Status: StatusPending, MaxAttempt: 1},
		{ID: "a_2", AgentID: "a", Name: "right", Status: StatusPending, MaxAttempt: 1},
	}}

	done := make(chan error, 1)
	go func() { done <- e.Run(context.Background(), dag, exec) }()

	// Both nodes must enter Execute before either is released.
	select {
	case <-exec.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first node never started")
	}
	select {
	case <-exec.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second node did not run concurrently; engine executed serially")
	}
	close(exec.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if exec.hits.Load() != 2 {
		t.Fatalf("expected 2 executions, got %d", exec.hits.Load())
	}
}

type failingThenExec struct{ failIDs map[string]bool }

func (f failingThenExec) Execute(ctx context.Context, node *Node) (map[string]any, error) {
	if f.failIDs[node.ID] {
		return nil, &execErr{node.ID}
	}
	return map[string]any{"node": node.Name}, nil
}

type execErr struct{ id string }

func (e *execErr) Error() string { return "boom: " + e.id }

func TestEnginePartialFailureBlocksDownstream(t *testing.T) {
	ns := newMemStore()
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	e := NewEngine(ns, bus, slog.New(slog.DiscardHandler), 2, nil)

	dag := &DAG{AgentID: "a", Nodes: []*Node{
		{ID: "a_root", AgentID: "a", Name: "root", Status: StatusPending, MaxAttempt: 1},
		{ID: "a_fail", AgentID: "a", Name: "fail", Status: StatusPending, MaxAttempt: 1, DependsOn: []string{"a_root"}},
		{ID: "a_ok", AgentID: "a", Name: "ok", Status: StatusPending, MaxAttempt: 1, DependsOn: []string{"a_root"}},
		{ID: "a_merge", AgentID: "a", Name: "merge", Status: StatusPending, MaxAttempt: 1, DependsOn: []string{"a_fail", "a_ok"}},
	}}

	err := e.Run(context.Background(), dag, failingThenExec{failIDs: map[string]bool{"a_fail": true}})
	if err == nil {
		t.Fatal("expected engine to report failure")
	}

	if ns.status("a_fail") != string(StatusFailed) {
		t.Fatalf("a_fail should be failed, got %s", ns.status("a_fail"))
	}
	if ns.status("a_ok") != string(StatusDone) {
		t.Fatalf("independent branch a_ok should still succeed, got %s", ns.status("a_ok"))
	}
	if ns.status("a_merge") != string(StatusBlocked) {
		t.Fatalf("a_merge should be blocked by failed dependency, got %s", ns.status("a_merge"))
	}
}
