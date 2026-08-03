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

func TestWorkerEmitsModelRoutedEvents(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n"), 0o644)

	provider := llm.NewScriptedProvider("scripted",
		llm.Response{ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Function: llm.Function{Name: "read_file", Arguments: `{"path":"go.mod"}`},
		}}},
		llm.Response{Content: "done"},
	)
	rtr := router.FromProvider(provider)

	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	sub, cancel := bus.Subscribe()
	defer cancel()

	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)
	w := New(Config{
		MaxIterations: 5,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
		Checkpoints:   checkpoint.NewInMemory(),
	}, rtr, tools.NewRegistry(tools.ReadFileTool{}), tasks.NewRegistry(),
		ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "a_r1_analyze", AgentID: "a", Name: "analyze", Template: "analyze",
		Inputs: map[string]any{"repo_path": repoDir, "goal": "analyze"},
	}
	if _, err := w.Run(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	var routed []events.Event
	for {
		select {
		case e := <-sub:
			if e.Type == events.ModelRouted {
				routed = append(routed, e)
				if len(routed) == 2 {
					goto done
				}
			}
		default:
			goto done
		}
	}
done:
	if len(routed) == 0 {
		t.Fatal("expected at least one model_routed event")
	}
	first := routed[0]
	if first.Payload["tier"] != string(router.TierFast) {
		t.Fatalf("analyze should route to the fast tier, got %v", first.Payload["tier"])
	}
	if first.Payload["gateway"] != "scripted" {
		t.Fatalf("expected the scripted gateway, got %v", first.Payload["gateway"])
	}
}
