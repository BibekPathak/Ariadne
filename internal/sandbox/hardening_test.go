package sandbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// The hardening assertions need a real Docker daemon. Run with:
//
//	SANDBOX_TEST_DOCKER=1 go test ./internal/sandbox/ -run Hardening -v
func TestDockerHardeningContainment(t *testing.T) {
	if os.Getenv("SANDBOX_TEST_DOCKER") == "" {
		t.Skip("set SANDBOX_TEST_DOCKER=1 to run docker hardening tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	repo := t.TempDir()
	s := NewDockerSandbox(DockerConfig{
		Image: "kubeai-sandbox:local", CPU: "1", MemMB: 512, Network: "none",
		ReadOnlyRoot: true, CapDropAll: true, PidsLimit: 256, RunAsUser: "1000:1000",
	})
	sess, err := s.Prepare(ctx, &RepoSource{LocalPath: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Destroy(context.Background()) }()

	t.Run("runs as non-root", func(t *testing.T) {
		out, err := sess.Exec(ctx, "/bin/sh", "-c", "id -u")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.Stdout) != "1000" {
			t.Fatalf("expected uid 1000, got %q", out.Stdout)
		}
	})

	t.Run("has no network interfaces beyond loopback", func(t *testing.T) {
		out, err := sess.Exec(ctx, "/bin/sh", "-c", "ls /sys/class/net")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.Stdout) != "lo" {
			t.Fatalf("expected only loopback, got %q", out.Stdout)
		}
	})

	t.Run("root filesystem is read-only", func(t *testing.T) {
		out, _ := sess.Exec(ctx, "/bin/sh", "-c", "touch /should-fail 2>&1; echo exit=$?")
		if strings.Contains(out.Stdout, "exit=0") {
			t.Fatalf("root filesystem should be read-only, got: %q", out.Stdout)
		}
	})

	t.Run("repo and tmpfs are writable", func(t *testing.T) {
		if err := sess.WriteFile(ctx, "/repo/w.txt", []byte("hi")); err != nil {
			t.Fatalf("bind mount should be writable: %v", err)
		}
		if _, err := sess.Exec(ctx, "/bin/sh", "-c", "echo ok > /tmp/probe.txt && cat /tmp/probe.txt"); err != nil {
			t.Fatalf("tmpfs should be writable: %v", err)
		}
	})

	t.Run("tmpfs supports exec", func(t *testing.T) {
		// Regression: Docker's default tmpfs is noexec, which broke running
		// toolchain binaries (go test) from /tmp inside hardened sandboxes.
		// Write via the shell (docker cp cannot reach tmpfs mounts in a
		// read-only rootfs).
		out, err := sess.Exec(ctx, "/bin/sh", "-c",
			"printf '#!/bin/sh\\necho exec-ok\\n' > /tmp/execprobe.sh && chmod +x /tmp/execprobe.sh && /tmp/execprobe.sh")
		if err != nil {
			t.Fatalf("tmpfs should be exec-mountable: %v", err)
		}
		if !strings.Contains(out.Stdout, "exec-ok") {
			t.Fatalf("unexpected output %q", out.Stdout)
		}
	})

	t.Run("fork bomb is contained by pids limit", func(t *testing.T) {
		out, _ := sess.Exec(ctx, "/bin/sh", "-c",
			"for i in $(seq 1 2000); do sleep 60 & done; wait; echo survived")
		// Either the fork errors or the container is killed — but the sandbox
		// must not be able to exhaust the host.
		if strings.Contains(out.Stdout, "survived") {
			t.Fatalf("expected fork attempts to hit the pids limit, got: %q", out.Stdout)
		}
	})
}
