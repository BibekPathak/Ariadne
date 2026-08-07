package autoscale

import (
	"log/slog"
	"testing"
	"time"
)

func TestAutoscalerEnsuresMin(t *testing.T) {
	depth := func() int64 { return 0 }
	a := NewAutoscaler(NewManager("/bin/sleep", slog.New(slog.DiscardHandler)), depth, 1, 3, 1, 10*time.Millisecond, slog.New(slog.DiscardHandler))
	a.tick()
	if a.m.Count() < 1 {
		t.Fatalf("expected at least min workers, got %d", a.m.Count())
	}
}

func TestAutoscalerScalesUpUnderLoad(t *testing.T) {
	a := NewAutoscaler(NewManager("/bin/sleep", slog.New(slog.DiscardHandler)), func() int64 { return 2 }, 1, 3, 1, 10*time.Millisecond, slog.New(slog.DiscardHandler))
	a.tick() // ensures min (1)
	a.tick() // depth 2 > threshold 1 -> scale up
	if a.m.Count() != 2 {
		t.Fatalf("expected 2 workers (min + scale up), got %d", a.m.Count())
	}
}

func TestAutoscalerScalesDownWhenIdle(t *testing.T) {
	loaded := true
	depth := func() int64 {
		if loaded {
			return 2
		}
		return 0
	}
	a := NewAutoscaler(NewManager("/bin/sleep", slog.New(slog.DiscardHandler)), depth, 1, 3, 1, 10*time.Millisecond, slog.New(slog.DiscardHandler))
	a.tick()
	a.tick() // 2 workers
	loaded = false
	// Pretend the workers have been idle for a while.
	a.mu.Lock()
	a.lastActivity = time.Now().Add(-time.Second)
	a.mu.Unlock()
	a.tick()
	if a.m.Count() != 1 {
		t.Fatalf("expected scale-down to min (1), got %d", a.m.Count())
	}
}
