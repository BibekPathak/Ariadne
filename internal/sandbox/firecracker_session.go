package sandbox

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// agentRequest/agentResponse mirror the protocol served by the in-VM
// sandbox-agent (cmd/sandbox-agent).
type agentRequest struct {
	Op      string `json:"op"`
	Command string `json:"command,omitempty"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"` // base64 for write
}

type agentResponse struct {
	OK      bool     `json:"ok"`
	Exit    int      `json:"exit,omitempty"`
	Stdout  string   `json:"stdout,omitempty"`
	Stderr  string   `json:"stderr,omitempty"`
	Content string   `json:"content,omitempty"`
	Entries []string `json:"entries,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// fcVM is one running microVM.
type fcVM struct {
	id        string
	dir       string
	vsockPath string
	port      int
	proc      *exec.Cmd
}

// rpc opens a host->guest vsock channel (AF_UNIX connect + CONNECT handshake),
// exchanges one request/response pair and closes.
func (v *fcVM) rpc(ctx context.Context, req *agentRequest) (*agentResponse, error) {
	if v == nil {
		return nil, fmt.Errorf("nil vm")
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", v.vsockPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", v.port); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("vsock handshake: %w", err)
	}
	if !strings.HasPrefix(line, "OK ") {
		return nil, fmt.Errorf("vsock handshake rejected: %q", line)
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	var resp agentResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// fcSession is a leased microVM with the task's repository provisioned.
type fcSession struct {
	sandbox *FirecrackerSandbox
	vm      *fcVM
	ctx     context.Context
}

func (s *fcSession) Exec(ctx context.Context, name string, args ...string) (Output, error) {
	parts := append([]string{name}, args...)
	return s.ExecShell(ctx, strings.Join(parts, " "))
}

func (s *fcSession) ExecShell(ctx context.Context, command string) (Output, error) {
	resp, err := s.vm.rpc(ctx, &agentRequest{Op: "exec", Command: command})
	if err != nil {
		return Output{}, err
	}
	if !resp.OK {
		return Output{}, fmt.Errorf("sandbox exec: %s", resp.Error)
	}
	return Output{Stdout: resp.Stdout, Stderr: resp.Stderr, ExitCode: resp.Exit}, nil
}

func (s *fcSession) ReadFile(ctx context.Context, path string) ([]byte, error) {
	resp, err := s.vm.rpc(ctx, &agentRequest{Op: "read", Path: path})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("sandbox read %s: %s", path, resp.Error)
	}
	return base64.StdEncoding.DecodeString(resp.Content)
}

func (s *fcSession) WriteFile(ctx context.Context, path string, data []byte) error {
	resp, err := s.vm.rpc(ctx, &agentRequest{
		Op: "write", Path: path, Content: base64.StdEncoding.EncodeToString(data),
	})
	if err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("sandbox write %s: %s", path, resp.Error)
	}
	return nil
}

func (s *fcSession) ListFiles(ctx context.Context, dir string) ([]string, error) {
	resp, err := s.vm.rpc(ctx, &agentRequest{Op: "list", Path: dir})
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("sandbox list %s: %s", dir, resp.Error)
	}
	return resp.Entries, nil
}

func (s *fcSession) CopyOut(ctx context.Context, sandboxPath, hostPath string) error {
	data, err := s.ReadFile(ctx, sandboxPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hostDir(hostPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(hostPath, data, 0o644)
}

func (s *fcSession) Destroy(ctx context.Context) error {
	s.sandbox.shutdown(s.vm)
	s.vm = nil
	return nil
}

func hostDir(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "."
	}
	return p[:i]
}
