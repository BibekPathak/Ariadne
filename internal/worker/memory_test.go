package worker

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"adriane/internal/artifacts"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/memory"
	"adriane/internal/sandbox"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/workflow"
)

type fakeMemory struct {
	long        []memory.Entry
	short       []memory.Entry
	semantic    []memory.Entry
	storedLong  []memory.Entry
	storedShort []memory.Entry
	indexed     int
	searched    int
}

func (f *fakeMemory) StoreShortTerm(ctx context.Context, agentID string, entries []memory.Entry) error {
	f.storedShort = append(f.storedShort, entries...)
	return nil
}
func (f *fakeMemory) LoadShortTerm(ctx context.Context, agentID string, limit int) ([]memory.Entry, error) {
	return f.short, nil
}
func (f *fakeMemory) StoreLongTerm(ctx context.Context, agentID string, e memory.Entry) error {
	f.storedLong = append(f.storedLong, e)
	return nil
}
func (f *fakeMemory) LoadLongTerm(ctx context.Context, agentID string, topics []string, limit int) ([]memory.Entry, error) {
	if len(topics) == 0 {
		return nil, nil
	}
	return f.long, nil
}
func (f *fakeMemory) IndexSemantic(ctx context.Context, agentID string, items []memory.IndexItem) error {
	f.indexed += len(items)
	return nil
}
func (f *fakeMemory) SearchSemantic(ctx context.Context, agentID string, query string, limit int) ([]memory.Entry, error) {
	f.searched++
	return f.semantic, nil
}
func (f *fakeMemory) Close() error { return nil }

func TestWorkerLoadsAndStoresMemory(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n"), 0o644)

	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)

	mem := &fakeMemory{long: []memory.Entry{
		{Kind: memory.KindPreference, Topic: "style", Content: "always prefer table driven tests"},
	}}
	w := New(Config{
		MaxIterations: 5,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
		Memory:        mem,
		UseSemantic:   true,
	}, llm.DemoProvider{}, "", tools.NewRegistry(tools.ListFilesTool{}, tools.ReadFileTool{}),
		tasks.NewRegistry(), ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "agent1_analyze", AgentID: "agent1", Name: "analyze", Template: "analyze",
		Inputs:     map[string]any{"repo_path": repoDir, "goal": "analyze repo using table driven test preferences"},
		MaxAttempt: 1,
	}

	out, err := w.Run(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	final, _ := out["final_message"].(string)
	if !strings.Contains(final, "table driven tests") {
		t.Fatalf("expected recollection from memory in final message, got %q", final)
	}
	if mem.searched == 0 {
		t.Fatal("expected a semantic search at load time")
	}
	if len(mem.storedLong) == 0 {
		t.Fatal("expected a long-term outcome to be stored")
	}
	if len(mem.storedShort) == 0 {
		t.Fatal("expected a short-term conversation to be stored")
	}
	if mem.indexed == 0 {
		t.Fatal("expected semantic indexing at store time")
	}
}

func TestWorkerNoMemoryNoop(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module x\n"), 0o644)
	bus := events.NewInMemoryBus(nil)
	defer bus.Close()
	arts, _ := artifacts.New(filepath.Join(t.TempDir(), "arts"), nil)

	w := New(Config{
		MaxIterations: 5,
		RepoBaseDir:   t.TempDir(),
		Sandbox:       sandbox.FakeSandbox{},
	}, llm.DemoProvider{}, "", tools.NewRegistry(tools.ListFilesTool{}),
		tasks.NewRegistry(), ctxbuilder.New(), arts, bus, slog.New(slog.DiscardHandler))

	node := &workflow.Node{
		ID: "a_t", AgentID: "a", Name: "analyze", Template: "analyze",
		Inputs: map[string]any{"repo_path": repoDir, "goal": "analyze"},
	}
	if _, err := w.Run(context.Background(), node); err != nil {
		t.Fatal(err)
	}
}

func TestTopicsFrom(t *testing.T) {
	got := topicsFrom("Prefer table-driven tests in this project")
	if len(got) == 0 {
		t.Fatal("expected topics")
	}
	for _, w := range got {
		if len(w) < 4 {
			t.Fatalf("topic %q too short", w)
		}
	}
	seen := map[string]bool{}
	for _, w := range got {
		if seen[w] {
			t.Fatalf("duplicate topic %q", w)
		}
		seen[w] = true
	}
}
