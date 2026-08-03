package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DockerConfig struct {
	Image   string
	CPU     string
	MemMB   int
	Network string // "none" or "bridge"
	BaseDir string // host dir for temp repo mounts

	// Hardening (Phase 5). Defaults applied in NewDockerSandbox.
	ReadOnlyRoot bool   // read-only root filesystem + writable tmpfs
	CapDropAll   bool   // drop all Linux capabilities
	PidsLimit    int    // cap process count (fork-bomb containment)
	RunAsUser    string // non-root user as "uid:gid"
}

type DockerSandbox struct {
	cfg DockerConfig
}

func NewDockerSandbox(cfg DockerConfig) *DockerSandbox {
	if cfg.BaseDir == "" {
		cfg.BaseDir = filepath.Join(os.TempDir(), "kubeai-sandbox")
	}
	if cfg.PidsLimit <= 0 {
		cfg.PidsLimit = 256
	}
	if cfg.RunAsUser == "" {
		cfg.RunAsUser = "1000:1000"
	}
	return &DockerSandbox{cfg: cfg}
}

func (d *DockerSandbox) Name() string { return "docker" }

func docker(args ...string) (Output, error) {
	cmd := exec.Command("docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := Output{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			out.ExitCode = ee.ExitCode()
		}
		return out, fmt.Errorf("docker %s: %s", strings.Join(args, " "), strings.TrimSpace(out.Stderr))
	}
	return out, nil
}

func (d *DockerSandbox) Prepare(ctx context.Context, repo *RepoSource) (Session, error) {
	if err := os.MkdirAll(d.cfg.BaseDir, 0o755); err != nil {
		return nil, err
	}
	// A repo must exist on the host to bind-mount. The worker clones or
	// copies it into BaseDir before calling Prepare.
	hostRepo := repo.LocalPath
	if hostRepo == "" {
		return nil, fmt.Errorf("docker sandbox requires a prepared local repo path")
	}
	hostRepo, err := filepath.Abs(hostRepo)
	if err != nil {
		return nil, err
	}

	s := &dockerSession{sandbox: d, hostRepo: hostRepo, name: "kubeai-" + randomID(8)}
	// Create a paused container with an isolated filesystem, no network by
	// default, resource limits, and the repo bind-mounted read-write at /repo.
	network := d.cfg.Network
	if network == "" {
		network = "none"
	}
	createArgs := []string{"create", "--name", s.name, "--network", network, "--workdir", "/repo",
		"-v", hostRepo + ":/repo", "--cpus", d.cfg.CPU}
	if d.cfg.MemMB > 0 {
		createArgs = append(createArgs, "--memory", fmt.Sprintf("%dm", d.cfg.MemMB))
	}
	// Hardening: read-only root with a writable tmpfs, no capabilities, no
	// new privileges, a non-root user, and a process-count cap.
	if d.cfg.ReadOnlyRoot {
		createArgs = append(createArgs, "--read-only", "--tmpfs", "/tmp:rw,size=512m,mode=1777")
		createArgs = append(createArgs, "-e", "HOME=/tmp/home", "-e", "GOCACHE=/tmp/gocache")
	}
	if d.cfg.CapDropAll {
		createArgs = append(createArgs, "--cap-drop", "ALL", "--security-opt", "no-new-privileges")
	}
	if d.cfg.PidsLimit > 0 {
		createArgs = append(createArgs, "--pids-limit", fmt.Sprintf("%d", d.cfg.PidsLimit))
	}
	if d.cfg.RunAsUser != "" {
		createArgs = append(createArgs, "--user", d.cfg.RunAsUser)
	}
	createArgs = append(createArgs, d.cfg.Image, "sleep", "infinity")
	if _, err := docker(createArgs...); err != nil {
		return nil, fmt.Errorf("create sandbox container: %w", err)
	}
	if _, err := docker("start", s.name); err != nil {
		_, _ = docker("rm", "-f", s.name)
		return nil, fmt.Errorf("start sandbox container: %w", err)
	}
	if d.cfg.ReadOnlyRoot {
		// Ensure writable home/build-cache dirs exist for the non-root user.
		_, _ = docker("exec", s.name, "/bin/sh", "-c",
			"mkdir -p /tmp/home /tmp/gocache && chown -R "+d.cfg.RunAsUser+" /tmp/home /tmp/gocache 2>/dev/null || true")
	}
	return s, nil
}

type dockerSession struct {
	sandbox   *DockerSandbox
	name      string
	hostRepo  string
	destroyed bool
}

func (s *dockerSession) Exec(ctx context.Context, name string, args ...string) (Output, error) {
	full := append([]string{"exec", s.name, name}, args...)
	return docker(full...)
}

func (s *dockerSession) ExecShell(ctx context.Context, command string) (Output, error) {
	return docker("exec", s.name, "/bin/sh", "-c", command)
}

func (s *dockerSession) ReadFile(ctx context.Context, path string) ([]byte, error) {
	tmp := filepath.Join(os.TempDir(), "kubeai-cp-"+randomID(8))
	if _, err := docker("cp", s.name+":"+path, tmp); err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	data, err := os.ReadFile(tmp)
	_ = os.Remove(tmp)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *dockerSession) WriteFile(ctx context.Context, path string, data []byte) error {
	tmp := filepath.Join(os.TempDir(), "kubeai-cp-"+randomID(8))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err := docker("cp", tmp, s.name+":"+path); err != nil {
		return fmt.Errorf("write file %s: %w", path, err)
	}
	return nil
}

func (s *dockerSession) ListFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := docker("exec", s.name, "/bin/sh", "-c", "ls -1 "+dir)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out.Stdout), "\n")
	var files []string
	for _, l := range lines {
		if l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

func (s *dockerSession) CopyOut(ctx context.Context, sandboxPath, hostPath string) error {
	if _, err := docker("cp", s.name+":"+sandboxPath, hostPath); err != nil {
		return fmt.Errorf("copy out %s: %w", sandboxPath, err)
	}
	return nil
}

func (s *dockerSession) Destroy(ctx context.Context) error {
	if s.destroyed {
		return nil
	}
	s.destroyed = true
	_, err := docker("rm", "-f", s.name)
	return err
}
