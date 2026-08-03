package worker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"adriane/internal/artifacts"
	"adriane/internal/checkpoint"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/router"
	"adriane/internal/sandbox"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/workflow"
)

func TestWorkerResumesFromCheckpoint(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n"), 0o644)

	// Seed a checkpoint as if the task was killed after one LLM round.
	cps := checkpoint.NewInMemory()
	_ = cps.Save(context.Background(), &checkpoint.Checkpoint{
		AgentID:   "agent1",
		TaskID:    "agent1_r1_analyze",
		Iteration: 1,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "goal"},
			{Role: llm.RoleAssistant, Content: "checkpointed-round", ToolCalls: []llm.ToolCall{{
				ID:       "call_1",
				Function: llm.Function{Name: "read_file", Arguments: `{"path":"go.mod"}`},
			}}},
			{Role: llm.RoleTool, ToolCallID: "call_1", Name: "read_file", Content: "module x"},
		},
	})

	// The provider only has the *remaining* work: one more tool call and a final.
	provider := llm.NewScriptedProvider("scripted",
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_2",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"go.mod"}`},
		}}},
		llm.Response{Content: "resumed and finished"},
	)

	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)

	w := New(Config{
		MaxIterations: 5,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
		Checkpoints:   cps,
	}, router.FromProvider(provider), tools.NewRegistry(tools.ReadFileTool{}), tasks.NewRegistry(),
		ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "agent1_r1_analyze", AgentID: "agent1", Name: "analyze", Template: "analyze",
		Inputs: map[string]any{"repo_path": repoDir, "goal": "goal"},
	}

	out, err := w.Run(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if out["final_message"] != "resumed and finished" {
		t.Fatalf("unexpected final message %v", out)
	}

	// The first request the provider saw must include the checkpointed history
	// (proving the loop resumed instead of restarting from scratch).
	if len(provider.Requests) == 0 {
		t.Fatal("provider received no requests")
	}
	first := provider.Requests[0]
	found := false
	for _, m := range first.Messages {
		if m.Role == llm.RoleAssistant && m.Content == "checkpointed-round" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("resumed run did not carry the checkpointed assistant message")
	}
	// Only the remaining iterations should have run (started at iteration 1).
	if len(provider.Requests) != 2 {
		t.Fatalf("expected exactly 2 remaining LLM calls, got %d", len(provider.Requests))
	}

	// Checkpoint must be cleared on success.
	cp, _ := cps.Load(context.Background(), node.ID)
	if cp != nil {
		t.Fatal("checkpoint should be deleted after successful completion")
	}
}

func TestWorkerStoresCheckpointDuringRun(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n"), 0o644)

	provider := llm.NewScriptedProvider("scripted",
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"go.mod"}`},
		}}},
		llm.Response{Content: "done"},
	)

	cps := checkpoint.NewInMemory()
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)

	w := New(Config{
		MaxIterations: 5,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
		Checkpoints:   cps,
	}, router.FromProvider(provider), tools.NewRegistry(tools.ReadFileTool{}), tasks.NewRegistry(),
		ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "a_r1_analyze", AgentID: "a", Name: "analyze", Template: "analyze",
		Inputs: map[string]any{"repo_path": repoDir, "goal": "goal"},
	}
	if _, err := w.Run(context.Background(), node); err != nil {
		t.Fatal(err)
	}
	// Completed successfully -> checkpoint removed.
	cp, _ := cps.Load(context.Background(), node.ID)
	if cp != nil {
		t.Fatal("expected checkpoint cleared after success")
	}
}
