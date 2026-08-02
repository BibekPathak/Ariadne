package worker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"adriane/internal/artifacts"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/sandbox"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/workflow"
)

func TestWorkerAgentLoopEndToEnd(t *testing.T) {
	// Script: the model first calls read_file, then produces a final answer.
	provider := llm.NewScriptedProvider("scripted",
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"notes.txt"}`},
		}}},
		llm.Response{Content: "I read the file, task done."},
	)

	repoDir := t.TempDir()
	if err := writeFile(repoDir, "notes.txt", "agent note"); err != nil {
		t.Fatal(err)
	}

	bus := events.NewInMemoryBus(nil)
	defer bus.Close()

	arts, err := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)
	if err != nil {
		t.Fatal(err)
	}

	w := New(Config{
		MaxIterations: 10,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
	}, provider, "", tools.NewRegistry(tools.ReadFileTool{}), tasks.NewRegistry(),
		ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID:         "agent1_analyze",
		AgentID:    "agent1",
		Name:       "analyze",
		Template:   "analyze",
		Inputs:     map[string]any{"repo_path": repoDir, "goal": "analyze the notes"},
		MaxAttempt: 1,
	}

	out, err := w.Run(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if out["final_message"] != "I read the file, task done." {
		t.Fatalf("unexpected final message %v", out)
	}
}

func TestWorkerExceedsIterations(t *testing.T) {
	// Model keeps calling tools forever: the loop must terminate with an error.
	provider := llm.NewScriptedProvider("loop",
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"x"}`},
		}}},
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_2",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"x"}`},
		}}},
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_3",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"x"}`},
		}}},
	)

	repoDir := t.TempDir()
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)

	w := New(Config{
		MaxIterations: 2,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
	}, provider, "", tools.NewRegistry(tools.ReadFileTool{}), tasks.NewRegistry(),
		ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "a1_t", AgentID: "agent1", Name: "t", Template: "analyze",
		Inputs: map[string]any{"repo_path": repoDir, "goal": "do it"},
	}
	if _, err := w.Run(context.Background(), node); err == nil {
		t.Fatal("expected iteration limit error")
	}
}

func writeFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}
