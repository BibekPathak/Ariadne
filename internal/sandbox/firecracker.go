package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// FirecrackerConfig configures the microVM sandbox.
type FirecrackerConfig struct {
	Binary      string // firecracker binary
	Kernel      string // vmlinux path
	RootFS      string // base rootfs.ext4 (contains /sandbox-agent as PID 1)
	WorkDir     string // scratch dir for per-VM sockets/rootfs copies
	VCPU        int
	MemMiB      int
	Port        int // vsock agent port inside the VM
	PoolSize    int // warm-pool size
	BootTimeout time.Duration
}

type FirecrackerSandbox struct {
	cfg FirecrackerConfig
	log *slog.Logger

	pool    chan *fcVM
	stop    chan struct{}
	started atomic.Bool
	mu      sync.Mutex
	booted  int
}

func NewFirecrackerSandbox(cfg FirecrackerConfig, logger *slog.Logger) *FirecrackerSandbox {
	if cfg.Binary == "" {
		cfg.Binary = "firecracker"
	}
	if cfg.Port == 0 {
		cfg.Port = 5200
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 2
	}
	if cfg.BootTimeout <= 0 {
		cfg.BootTimeout = 60 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FirecrackerSandbox{cfg: cfg, log: logger, pool: make(chan *fcVM, cfg.PoolSize), stop: make(chan struct{})}
}

func (f *FirecrackerSandbox) Name() string { return "firecracker" }

// ensure starts the warm-pool refiller once.
func (f *FirecrackerSandbox) ensure() {
	if f.started.Swap(true) {
		return
	}
	if err := os.MkdirAll(f.cfg.WorkDir, 0o755); err != nil {
		f.log.Error("firecracker workdir", "err", err)
		return
	}
	go f.refill()
}

func (f *FirecrackerSandbox) refill() {
	for {
		select {
		case <-f.stop:
			return
		default:
		}
		vm := f.bootVM()
		if vm == nil {
			// Boot failed; back off briefly and retry.
			select {
			case <-time.After(500 * time.Millisecond):
			case <-f.stop:
				return
			}
			continue
		}
		select {
		case f.pool <- vm:
		case <-f.stop:
			f.shutdown(vm)
			return
		}
	}
}

func (f *FirecrackerSandbox) Prepare(ctx context.Context, repo *RepoSource) (Session, error) {
	f.ensure()
	var vm *fcVM
	select {
	case vm = <-f.pool:
	case <-f.stop:
		return nil, fmt.Errorf("firecracker sandbox stopped")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if vm == nil {
		return nil, fmt.Errorf("firecracker VM boot failed")
	}
	f.mu.Lock()
	f.booted++
	f.mu.Unlock()

	// Provision the repository into the VM's /repo over vsock.
	if repo != nil && repo.LocalPath != "" {
		if err := f.uploadRepo(ctx, vm, repo.LocalPath); err != nil {
			f.shutdown(vm)
			return nil, fmt.Errorf("provision repo: %w", err)
		}
	}
	return &fcSession{sandbox: f, vm: vm, ctx: ctx}, nil
}

func (f *FirecrackerSandbox) uploadRepo(ctx context.Context, vm *fcVM, hostPath string) error {
	return filepath.Walk(hostPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(hostPath, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		resp, err := vm.rpc(ctx, &agentRequest{
			Op: "write", Path: filepath.Join("/repo", rel), Content: base64.StdEncoding.EncodeToString(data),
		})
		if err != nil {
			return err
		}
		if !resp.OK {
			return fmt.Errorf("upload %s: %s", rel, resp.Error)
		}
		return nil
	})
}

func (f *FirecrackerSandbox) shutdown(vm *fcVM) {
	if vm == nil {
		return
	}
	if vm.proc != nil {
		_ = vm.proc.Process.Signal(os.Interrupt)
		select {
		case <-time.After(3 * time.Second):
			_ = vm.proc.Process.Kill()
		default:
		}
	}
	_ = os.RemoveAll(vm.dir)
}

// Close stops the warm pool and shuts down any spare VMs.
func (f *FirecrackerSandbox) Close() {
	close(f.stop)
	for {
		select {
		case vm := <-f.pool:
			f.shutdown(vm)
		default:
			return
		}
	}
}

// bootVM starts a microVM and waits until the in-VM agent answers a ping.
func (f *FirecrackerSandbox) bootVM() *fcVM {
	start := time.Now()
	id := randomID(8)
	dir, err := filepath.Abs(filepath.Join(f.cfg.WorkDir, "vm-"+id))
	if err != nil {
		f.log.Error("abs workdir", "err", err)
		return nil
	}
	_ = os.MkdirAll(dir, 0o755)
	fail := func(format string, args ...any) *fcVM {
		f.log.Error(format, args...)
		_ = os.RemoveAll(dir)
		return nil
	}

	rootfs := filepath.Join(dir, "rootfs.ext4")
	if err := copyFile(f.cfg.RootFS, rootfs); err != nil {
		return fail("copy rootfs", "err", err)
	}
	vsockPath := filepath.Join(dir, "v.sock")
	apiSock := filepath.Join(dir, "api.sock")
	cfgPath := filepath.Join(dir, "fc.json")

	// Firecracker resolves the kernel/rootfs paths relative to its own cwd
	// (the scratch dir), so pass absolute paths.
	kernelAbs, err := filepath.Abs(f.cfg.Kernel)
	if err != nil {
		return fail("abs kernel", "err", err)
	}
	rootfsAbs, err := filepath.Abs(rootfs)
	if err != nil {
		return fail("abs rootfs", "err", err)
	}
	if err := writeVMConfig(cfgPath, f.cfg, kernelAbs, rootfsAbs, vsockPath); err != nil {
		return fail("write vm config", "err", err)
	}

	vm := &fcVM{id: id, dir: dir, vsockPath: vsockPath, port: f.cfg.Port}

	logFile, err := os.Create(filepath.Join(dir, "boot.log"))
	if err != nil {
		return fail("create boot log", "err", err)
	}
	binary, err := filepath.Abs(f.cfg.Binary)
	if err != nil {
		logFile.Close()
		return fail("abs binary", "err", err)
	}
	cmd := exec.Command(binary, "--config-file", cfgPath, "--api-sock", apiSock)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fail("start firecracker", "err", err)
	}
	vm.proc = cmd

	deadline := time.Now().Add(f.cfg.BootTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(vsockPath); err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// Socket exists — wait for the agent to be reachable.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := vm.rpc(ctx, &agentRequest{Op: "ping"})
		cancel()
		if err == nil && resp.OK {
			f.log.Info("firecracker VM ready", "id", id, "boot_ms", time.Since(start).Milliseconds())
			logFile.Close()
			return vm
		}
		time.Sleep(150 * time.Millisecond)
	}
	f.log.Error("firecracker VM failed to become ready", "id", id)
	logFile.Close()
	f.shutdown(vm)
	return nil
}

func writeVMConfig(path string, cfg FirecrackerConfig, kernelAbs, rootfsAbs, vsockPath string) error {
	conf := map[string]any{
		"boot-source": map[string]any{
			"kernel_image_path": kernelAbs,
			"boot_args":         "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/sandbox-agent",
		},
		"drives": []any{map[string]any{
			"drive_id": "root", "path_on_host": rootfsAbs, "is_root_device": true, "is_read_only": false,
		}},
		"machine-config": map[string]any{
			"vcpu_count": cfg.VCPU, "mem_size_mib": cfg.MemMiB,
		},
		"vsock": map[string]any{
			"guest_cid": 3, "uds_path": vsockPath,
		},
	}
	raw, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
