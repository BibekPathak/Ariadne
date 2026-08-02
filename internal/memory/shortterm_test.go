package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestShortTermRedisStoreAndLoad(t *testing.T) {
	mr := miniredis.RunT(t)
	// miniredis serves on a unix socket; rewrite to tcp so go-redis is happy.
	addr := mr.Addr()

	s, err := NewShortTermRedis(addr, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	entries := []Entry{{Kind: KindConversation, Topic: "analyze", Content: "inspected repo"}}
	if err := s.StoreShortTerm(ctx, "agent1", entries); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadShortTerm(ctx, "agent1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "inspected repo" {
		t.Fatalf("unexpected short-term memory %+v", got)
	}

	// Newer entries are prepended.
	more := []Entry{{Kind: KindOutcome, Topic: "implement", Content: "added function"}}
	if err := s.StoreShortTerm(ctx, "agent1", more); err != nil {
		t.Fatal(err)
	}
	got, _ = s.LoadShortTerm(ctx, "agent1", 10)
	if len(got) != 2 || got[0].Content != "added function" {
		t.Fatalf("expected newest first, got %+v", got)
	}
}

func TestShortTermRedisTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	s, _ := NewShortTermRedis(mr.Addr(), time.Hour)
	defer s.Close()
	ctx := context.Background()
	_ = s.StoreShortTerm(ctx, "agent1", []Entry{{Kind: KindConversation, Content: "x"}})
	ttl := mr.TTL("agent:st:agent1")
	if ttl != time.Hour {
		t.Fatalf("expected 1h TTL, got %s", ttl)
	}
}

func TestShortTermRedisEmptyAgent(t *testing.T) {
	mr := miniredis.RunT(t)
	s, _ := NewShortTermRedis(mr.Addr(), time.Hour)
	defer s.Close()
	got, err := s.LoadShortTerm(context.Background(), "nobody", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no memory, got %+v", got)
	}
}
