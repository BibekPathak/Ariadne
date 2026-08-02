package context

import (
	"testing"

	"kubeai/internal/llm"
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
		MaxTokens:    20, // tiny budget forces truncation
	})
	// Count how many history messages were kept.
	kept := 0
	for _, m := range req.Messages[2:] {
		if m.Role == llm.RoleUser {
			kept++
		}
	}
	if kept == 0 {
		t.Fatal("expected at least the newest history message to fit")
	}
	if kept >= 50 {
		t.Fatal("expected history to be truncated under tiny budget")
	}
	// The newest message must be present.
	last := req.Messages[len(req.Messages)-1]
	if last.Content != "m"+string(rune('a'+49)) {
		t.Fatalf("expected newest history message kept last, got %q", last.Content)
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("hello world") != 3 {
		t.Fatalf("expected 3, got %d", EstimateTokens("hello world"))
	}
}
