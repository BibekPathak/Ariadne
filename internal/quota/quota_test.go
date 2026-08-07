package quota

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"adriane/internal/events"
	"adriane/internal/store"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run quota integration tests")
	}
	st, err := store.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

// uid returns a run-unique agent id so tests are re-runnable.
func uid(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestConcurrentAgentCap(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	q := New(st, Limits{MaxConcurrentAgents: 2})

	for i := 0; i < 2; i++ {
		agent := &store.Agent{
			ID: uid("a"), OrgID: "qtest", Template: "coder",
			Goal: "g", Status: "running",
		}
		if err := st.Agents.Create(ctx, agent); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.CheckCreate(ctx, "qtest"); err == nil {
		t.Fatal("expected quota error at the concurrency cap")
	}
}

func TestDailyAgentCap(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	q := New(st, Limits{MaxDailyAgents: 1})

	agent := &store.Agent{ID: uid("d"), OrgID: "qday", Template: "coder", Goal: "g", Status: "completed"}
	if err := st.Agents.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if err := q.CheckCreate(ctx, "qday"); err == nil {
		t.Fatal("expected quota error at the daily agent cap")
	}
}

func TestNoCapAllowsCreate(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	q := New(st, Limits{}) // unlimited
	if err := q.CheckCreate(ctx, "nonexistent"); err != nil {
		t.Fatalf("unlimited quota should allow: %v", err)
	}
}

func TestCostQuota(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	q := New(st, Limits{DailyCostCap: 0.5, PricePer1k: 0.01})

	agent := &store.Agent{ID: uid("c"), OrgID: "qcost", Template: "coder", Goal: "g", Status: "completed"}
	if err := st.Agents.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	// 100k tokens at $0.01/1k = $1.00, over the $0.50 cap.
	ev := events.New(agent.ID, "", events.LLMCalled, map[string]any{"total_tokens": 100_000})
	if _, err := st.Events.Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := q.CheckCreate(ctx, "qcost"); err == nil {
		t.Fatal("expected quota error over the daily cost cap")
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}
