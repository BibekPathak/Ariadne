package leader

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLeaderFailover(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run leader election tests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// cancel() must run before pool.Close() so the election goroutines release
	// their held advisory-lock connection (LIFO defer order).
	defer cancel()

	lead := New(pool, slog.New(slog.DiscardHandler))
	standby := New(pool, slog.New(slog.DiscardHandler))

	var leadCount, standbyCount int
	leadDone := make(chan struct{})
	standbyDone := make(chan struct{})

	go func() {
		lead.Run(ctx, func(c context.Context) { leadCount++ })
		close(leadDone)
	}()
	go func() {
		standby.Run(ctx, func(c context.Context) { standbyCount++ })
		close(standbyDone)
	}()

	// Both try to acquire; exactly one should be the leader.
	time.Sleep(3 * time.Second)
	if lead.IsLeader() == standby.IsLeader() {
		t.Fatalf("expected exactly one leader, got lead=%v standby=%v", lead.IsLeader(), standby.IsLeader())
	}
	if leadCount+standbyCount != 1 {
		t.Fatalf("expected exactly one onLeader callback, got lead=%d standby=%d", leadCount, standbyCount)
	}

	// Kill the leader; the standby must acquire and become leader.
	lead.Close()
	time.Sleep(4 * time.Second)
	if !standby.IsLeader() {
		t.Fatal("standby should have acquired leadership after leader died")
	}
	if standbyCount != 1 {
		t.Fatalf("standby onLeader should fire exactly once, got %d", standbyCount)
	}
}
