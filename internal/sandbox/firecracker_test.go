package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Live test for the Firecracker runtime. Run with:
//
//	SANDBOX_TEST_FIRECRACKER=1 go test ./internal/sandbox/ -run Firecracker -v
func TestFirecrackerSandboxExec(t *testing.T) {
	if os.Getenv("SANDBOX_TEST_FIRECRACKER") == "" {
		t.Skip("set SANDBOX_TEST_FIRECRACKER=1 to run firecracker tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fc := func(rel string) string { return filepath.Join(moduleRoot(t), "deploy", "firecracker", rel) }
	sb := NewFirecrackerSandbox(FirecrackerConfig{
		Binary:      fc("firecracker"),
		Kernel:      fc("vmlinux.bin"),
		RootFS:      fc("rootfs.ext4"),
		WorkDir:     t.TempDir(),
		VCPU:        2,
		MemMiB:      1024,
		Port:        5200,
		PoolSize:    1,
		BootTimeout: 90 * time.Second,
	}, nil)
	defer sb.Close()

	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "go.mod"), "module demoproj\n\ngo 1.25\n")
	mustWrite(t, filepath.Join(repo, "mathx/mathx.go"),
		"package mathx\n\nfunc Add(a, b int) int { return a + b }\n")
	mustWrite(t, filepath.Join(repo, "mathx/mathx_test.go"),
		"package mathx\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1,2) != 3 { t.Fatal(\"bad\") }\n}\n")

	sess, err := sb.Prepare(ctx, &RepoSource{LocalPath: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Destroy(context.Background()) }()

	t.Run("toolchain present", func(t *testing.T) {
		out, err := sess.ExecShell(ctx, "go version")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Stdout, "go") {
			t.Fatalf("unexpected: %q", out.Stdout)
		}
	})

	t.Run("repo provisioned", func(t *testing.T) {
		data, err := sess.ReadFile(ctx, "/repo/go.mod")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "demoproj") {
			t.Fatalf("unexpected go.mod: %s", data)
		}
	})

	t.Run("write and read back", func(t *testing.T) {
		if err := sess.WriteFile(ctx, "/repo/note.txt", []byte("hi from vm")); err != nil {
			t.Fatal(err)
		}
		data, err := sess.ReadFile(ctx, "/repo/note.txt")
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hi from vm" {
			t.Fatalf("round-trip mismatch: %q", data)
		}
	})

	t.Run("go test runs in the VM", func(t *testing.T) {
		out, err := sess.ExecShell(ctx, "cd /repo && go test ./... 2>&1")
		if err != nil {
			t.Fatal(err)
		}
		if out.ExitCode != 0 || !strings.Contains(out.Stdout, "ok") {
			t.Fatalf("go test failed: exit=%d out=%q", out.ExitCode, out.Stdout)
		}
	})
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// moduleRoot walks up from the test's working directory to the module root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root")
		}
		dir = parent
	}
}
