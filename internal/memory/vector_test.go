package memory

import (
	"context"
	"testing"

	"adriane/internal/llm"
)

func TestVectorStoreIndexAndSearch(t *testing.T) {
	v, err := NewVectorStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	apple := "prefer table driven tests"
	banana := "use camelCase variables"
	_ = v.Index(ctx, []IndexItem{
		{ID: "a", AgentID: "agent1", Kind: KindPreference, Topic: "style", Content: apple, Vector: DeterministicEmbedFn(apple)},
		{ID: "b", AgentID: "agent1", Kind: KindPreference, Topic: "style", Content: banana, Vector: DeterministicEmbedFn(banana)},
	})

	res, err := v.Search(ctx, "agent1", DeterministicEmbedFn("table driven tests"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected at least one result")
	}
	if res[0].ID != "a" {
		t.Fatalf("expected the table-driven-tests memory to rank first, got %q", res[0].ID)
	}
	if res[0].Score <= 0 {
		t.Fatalf("expected a positive score, got %f", res[0].Score)
	}
}

func TestVectorStoreScopesByAgent(t *testing.T) {
	v, _ := NewVectorStore(t.TempDir())
	ctx := context.Background()
	_ = v.Index(ctx, []IndexItem{
		{ID: "x", AgentID: "other", Kind: KindOutcome, Content: "secret plan", Vector: DeterministicEmbedFn("secret plan")},
	})
	res, err := v.Search(ctx, "agent1", DeterministicEmbedFn("secret plan"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("agent1 must not see another agent's memory, got %d", len(res))
	}
}

func TestVectorStorePersists(t *testing.T) {
	dir := t.TempDir()
	v, _ := NewVectorStore(dir)
	_ = v.Index(context.Background(), []IndexItem{
		{ID: "p", AgentID: "a", Kind: KindOutcome, Content: "went well", Vector: DeterministicEmbedFn("went well")},
	})
	v2, err := NewVectorStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := v2.Search(context.Background(), "a", DeterministicEmbedFn("went well"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 {
		t.Fatalf("expected persisted point, got %d", len(res))
	}
}

// DeterministicEmbedFn mirrors the llm fake embed for tests without importing it.
func DeterministicEmbedFn(s string) []float32 {
	return llm.DeterministicEmbed([]string{s})[0]
}
