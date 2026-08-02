package sandbox

import "context"

// Output is the combined result of a command executed inside a sandbox.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Session is one isolated execution environment (container, VM, process).
type Session interface {
	// Exec runs a command in the sandbox working directory (/repo).
	Exec(ctx context.Context, name string, args ...string) (Output, error)
	// ExecShell runs a raw shell command string in the sandbox.
	ExecShell(ctx context.Context, command string) (Output, error)
	ReadFile(ctx context.Context, path string) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte) error
	ListFiles(ctx context.Context, dir string) ([]string, error)
	// CopyOut copies a path out of the sandbox onto the host.
	CopyOut(ctx context.Context, sandboxPath, hostPath string) error
	// Destroy tears the sandbox down and frees its resources.
	Destroy(ctx context.Context) error
}

// RepoSource describes where the task's repository lives. Exactly one of
// URL or LocalPath should be set.
type RepoSource struct {
	URL       string
	LocalPath string
}

// Sandbox is the factory for isolated execution environments. Docker is the
// Phase 1 implementation; Firecracker slots in behind this same interface.
type Sandbox interface {
	Name() string
	Prepare(ctx context.Context, repo *RepoSource) (Session, error)
}
