package events

import (
	"context"
	"log/slog"
	"testing"
)

func TestInMemoryBusFanout(t *testing.T) {
	bus := NewInMemoryBus(slog.New(slog.DiscardHandler))
	defer bus.Close()

	ch1, cancel1 := bus.Subscribe()
	defer cancel1()
	ch2, cancel2 := bus.Subscribe()
	defer cancel2()

	e := New("agent-1", "task-1", TaskStarted, map[string]any{"name": "analyze"})
	if err := bus.Publish(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	for i, ch := range []<-chan Event{ch1, ch2} {
		got := <-ch
		if got.AgentID != "agent-1" || got.Type != TaskStarted {
			t.Fatalf("subscriber %d got wrong event: %+v", i, got)
		}
		if got.Seq != 0 {
			t.Fatalf("expected seq 0 (no persister), got %d", got.Seq)
		}
	}
}

func TestInMemoryBusPersistsBeforeFanout(t *testing.T) {
	var persisted []Event
	bus := NewInMemoryBus(nil)
	defer bus.Close()
	bus.SetPersister(func(e Event) (int64, error) {
		seq := int64(len(persisted) + 1)
		e.Seq = seq
		persisted = append(persisted, e)
		return seq, nil
	})

	ch, cancel := bus.Subscribe()
	defer cancel()

	e := New("agent-1", "", LLMCalled, map[string]any{"total_tokens": 42})
	if err := bus.Publish(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.Seq != 1 {
		t.Fatalf("expected persisted seq assigned before fanout, got %d", got.Seq)
	}
	if len(persisted) != 1 {
		t.Fatalf("expected 1 persisted event, got %d", len(persisted))
	}
}

func TestInMemoryBusNoLeakAfterCancel(t *testing.T) {
	bus := NewInMemoryBus(nil)
	defer bus.Close()
	ch, cancel := bus.Subscribe()
	cancel()
	if err := bus.Publish(context.Background(), New("a", "", AgentCreated, nil)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
		t.Fatal("should not receive after cancel")
	default:
	}
}
