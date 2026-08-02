package context

import (
	"strings"
	"testing"

	"adriane/internal/llm"
)

func TestBuildKeepsSystemAndGoal(t *testing.T) {
	b := New()
	req := b.Build(Input{
		SystemPrompt: "system",
		Goal:         "goal",
		MaxTokens:    1000,
	})
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages (system+goal), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != llm.RoleSystem || req.Messages[1].Role != llm.RoleUser {
		t.Fatal("message roles wrong")
	}
}

func TestBuildTruncatesHistoryToBudget(t *testing.T) {
	b := New()
	var history []llm.Message
	for i := 0; i < 50; i++ {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: "m" + string(rune('a'+i))})
	}
	req := b.Build(Input{
		SystemPrompt: "sys",
		Goal:         "goal",
		Messages:     history,
		MaxTokens:    20,
	})
	kept := 0
	for _, m := range req.Messages[2:] {
		if m.Role == llm.RoleUser {
			kept++
		}
	}
	if kept == 0 || kept >= 50 {
		t.Fatalf("expected partial history kept, got %d", kept)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Content != "m"+string(rune('a'+49)) {
		t.Fatalf("expected newest history message last, got %q", last.Content)
	}
}

func TestBuildInjectsMemoriesIntoSystem(t *testing.T) {
	b := New()
	req := b.Build(Input{
		SystemPrompt: "sys",
		Goal:         "goal",
		Memories: []MemoryItem{
			{Kind: "preference", Content: "prefer table driven tests", Score: 0.9},
			{Kind: "outcome", Content: "previous refactor succeeded", Score: 0.4},
		},
		MaxTokens: 2000,
	})
	sys := req.Messages[0].Content
	if !strings.Contains(sys, "RECOLLECTION: prefer table driven tests") {
		t.Fatalf("expected high-scoring memory in system prompt, got:\n%s", sys)
	}
	if !strings.Contains(sys, "RECOLLECTION: previous refactor succeeded") {
		t.Fatalf("expected second memory in system prompt")
	}
	// High-scoring memory must come first (ranked).
	if strings.Index(sys, "prefer table driven tests") > strings.Index(sys, "previous refactor succeeded") {
		t.Fatal("memories should be ordered by score")
	}
}

func TestBuildDropsLowValueComponentsUnderBudget(t *testing.T) {
	b := New()
	req := b.Build(Input{
		SystemPrompt: "sys",
		Goal:         "goal",
		Memories: []MemoryItem{
			{Kind: "preference", Content: strings.Repeat("A", 2000), Score: 0.9},
			{Kind: "artifact", Content: strings.Repeat("B", 2000), Score: 0.1},
		},
		MaxTokens: 600, // only the high-value memory fits
	})
	sys := req.Messages[0].Content
	if !strings.Contains(sys, "AAAA") {
		t.Fatal("expected high-value memory to be included")
	}
	if strings.Contains(sys, "BBBB") {
		t.Fatal("expected low-value memory to be dropped under budget")
	}
}

func TestBuildCompressesOversizedContent(t *testing.T) {
	b := New()
	big := strings.Repeat("x", 10_000)
	req := b.Build(Input{
		SystemPrompt: "sys",
		Goal:         "goal",
		Memories:     []MemoryItem{{Kind: "outcome", Content: big, Score: 0.9}},
		MaxTokens:    5000,
	})
	sys := req.Messages[0].Content
	if len(sys) >= 10_000 {
		t.Fatalf("expected compression, got %d chars", len(sys))
	}
	if !strings.HasPrefix(sys[len(sys)-100:], "xxx") {
		t.Fatal("compression should keep the tail")
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("hello world") != 3 {
		t.Fatalf("expected 3, got %d", EstimateTokens("hello world"))
	}
}
