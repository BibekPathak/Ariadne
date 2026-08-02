package llm

import (
	"context"
	"testing"
)

func TestScriptedProviderReturnsQueueThenFinishes(t *testing.T) {
	p := NewScriptedProvider("test",
		Response{Content: "first", Usage: Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	)
	req := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}

	r1, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != "first" || r1.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected first response %+v", r1)
	}

	r2, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Content != "Done." {
		t.Fatalf("expected fallback 'Done.', got %q", r2.Content)
	}
}

func TestScriptedProviderCanCallTool(t *testing.T) {
	p := NewScriptedProvider("test",
		Response{ToolCalls: []ToolCall{{ID: "call_1", Function: Function{Name: "shell", Arguments: `{"command":"true"}`}}}},
		Response{Content: "finished"},
	)
	r1, err := p.Generate(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.ToolCalls) != 1 || r1.ToolCalls[0].Function.Name != "shell" {
		t.Fatalf("expected shell tool call, got %+v", r1.ToolCalls)
	}
}
