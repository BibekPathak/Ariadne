package tools

import (
	"context"
	"testing"

	"kubeai/internal/sandbox"
)

func TestReadWriteFile(t *testing.T) {
	s := sandbox.NewFakeSession(t.TempDir())
	ec := &ExecContext{Session: s, Workdir: ""}

	if _, err := (WriteFileTool{}).Execute(context.Background(), map[string]any{
		"path": "hello.txt", "content": "hi there",
	}, ec); err != nil {
		t.Fatal(err)
	}
	out, err := (ReadFileTool{}).Execute(context.Background(), map[string]any{"path": "hello.txt"}, ec)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi there" {
		t.Fatalf("expected content, got %q", out)
	}
}

func TestListFiles(t *testing.T) {
	s := sandbox.NewFakeSession(t.TempDir())
	ec := &ExecContext{Session: s, Workdir: ""}
	_ = s.WriteFile(context.Background(), "a.txt", []byte("a"))
	_ = s.WriteFile(context.Background(), "b.txt", []byte("b"))
	out, err := (ListFilesTool{}).Execute(context.Background(), map[string]any{"path": "."}, ec)
	if err != nil {
		t.Fatal(err)
	}
	if out != "a.txt\nb.txt" {
		t.Fatalf("unexpected listing %q", out)
	}
}

func TestShellTool(t *testing.T) {
	s := sandbox.NewFakeSession(t.TempDir())
	ec := &ExecContext{Session: s, Workdir: ""}
	out, err := (ShellTool{}).Execute(context.Background(), map[string]any{"command": "true"}, ec)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected shell output")
	}
}

func TestMarshalInput(t *testing.T) {
	in, err := MarshalInput(`{"path": "x", "content": "y"}`)
	if err != nil {
		t.Fatal(err)
	}
	if in["path"] != "x" || in["content"] != "y" {
		t.Fatalf("unexpected input %v", in)
	}
	if _, err := MarshalInput(""); err != nil {
		t.Fatal("empty input should be allowed")
	}
}

func TestRegistryDefinitionsAndExecute(t *testing.T) {
	r := NewRegistry(ReadFileTool{}, WriteFileTool{})
	defs := r.Definitions([]string{"read_file"})
	if len(defs) != 1 || defs[0].Function.Name != "read_file" {
		t.Fatalf("expected only read_file, got %+v", defs)
	}
	defs = r.Definitions(nil)
	if len(defs) != 2 {
		t.Fatalf("expected all tools, got %d", len(defs))
	}
	if _, err := r.Execute(context.Background(), "missing", nil, nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
}
