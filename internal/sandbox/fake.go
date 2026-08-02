package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FakeSession is an in-memory sandbox for tests. It keeps a filesystem in a
// temp dir so tools can be exercised without Docker.
type FakeSession struct {
	Root string
}

func NewFakeSession(tRoot string) *FakeSession {
	_ = os.MkdirAll(tRoot, 0o755)
	return &FakeSession{Root: tRoot}
}

func (f *FakeSession) Exec(ctx context.Context, name string, args ...string) (Output, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	return f.ExecShell(ctx, cmd)
}

func (f *FakeSession) ExecShell(ctx context.Context, command string) (Output, error) {
	out, err := os.Executable()
	if err != nil {
		return Output{}, err
	}
	_ = out
	if command == "pwd" || strings.HasPrefix(command, "pwd") {
		return Output{Stdout: f.Root, ExitCode: 0}, nil
	}
	return Output{ExitCode: 0, Stdout: fmt.Sprintf("ran: %s", command)}, nil
}

func (f *FakeSession) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(f.Root, path))
}

func (f *FakeSession) WriteFile(ctx context.Context, path string, data []byte) error {
	full := filepath.Join(f.Root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

func (f *FakeSession) ListFiles(ctx context.Context, dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(f.Root, dir))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

func (f *FakeSession) CopyOut(ctx context.Context, sandboxPath, hostPath string) error {
	data, err := f.ReadFile(ctx, sandboxPath)
	if err != nil {
		return err
	}
	return os.WriteFile(hostPath, data, 0o644)
}

func (f *FakeSession) Destroy(ctx context.Context) error {
	return nil
}

// FakeSandbox produces FakeSessions rooted in separate temp dirs.
type FakeSandbox struct{}

func (FakeSandbox) Name() string { return "fake" }

func (FakeSandbox) Prepare(ctx context.Context, repo *RepoSource) (Session, error) {
	root, err := os.MkdirTemp("", "kubeai-fake-")
	if err != nil {
		return nil, err
	}
	// Seed with the local repo contents if provided.
	if repo != nil && repo.LocalPath != "" {
		err := copyTree(repo.LocalPath, root)
		if err != nil {
			return nil, err
		}
	}
	return NewFakeSession(root), nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
