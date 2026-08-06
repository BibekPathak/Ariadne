package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"adriane/internal/events"
	"adriane/internal/workflow"
)

type fakeWorker struct {
	failFirst bool
	calls     atomic.Int32
}

func (f *fakeWorker) Run(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	n := f.calls.Add(1)
	if f.failFirst && n == 1 {
		return nil, errors.New("boom")
	}
	return map[string]any{"ok": true}, nil
}

func TestSchedulerRetriesThenSucceeds(t *testing.T) {
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	w := &fakeWorker{failFirst: true}
	s := NewScheduler(bus, w, slog.New(slog.DiscardHandler), 1, nil)

	node := &workflow.Node{AgentID: "a", ID: "t1", MaxAttempt: 3}
	out, err := s.Execute(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true {
		t.Fatalf("unexpected output %v", out)
	}
	if w.calls.Load() != 2 {
		t.Fatalf("expected 2 calls (1 fail + 1 ok), got %d", w.calls.Load())
	}
}

func TestSchedulerFailsAfterMaxAttempts(t *testing.T) {
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	w2 := &alwaysFail{}
	s := NewScheduler(bus, w2, slog.New(slog.DiscardHandler), 1, nil)

	node := &workflow.Node{AgentID: "a", ID: "t1", MaxAttempt: 2}
	if _, err := s.Execute(context.Background(), node); err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if w2.calls.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", w2.calls.Load())
	}
}

func TestSchedulerConcurrentDispatch(t *testing.T) {
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	w := &fakeWorker{}
	s := NewScheduler(bus, w, slog.New(slog.DiscardHandler), 2, nil)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Execute(context.Background(), &workflow.Node{AgentID: "a", ID: "t", MaxAttempt: 1})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent dispatch failed: %v", err)
		}
	}
	if w.calls.Load() != 2 {
		t.Fatalf("expected 2 executions, got %d", w.calls.Load())
	}
}

type alwaysFail struct{ calls atomic.Int32 }

func (f *alwaysFail) Run(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	f.calls.Add(1)
	return nil, errors.New("boom")
}
