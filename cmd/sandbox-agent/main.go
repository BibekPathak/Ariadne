// sandbox-agent runs as PID 1 inside a Firecracker microVM. It mounts the
// basic virtual filesystems, then serves a tiny command protocol over a
// vsock port: the host sends one JSON request per connection and receives one
// JSON response. This is the tool runtime bootstrap for the Firecracker
// sandbox.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mdlayher/vsock"
)

const port = 5200

type Request struct {
	Op      string `json:"op"` // exec | read | write | list | ping
	Command string `json:"command"`
	Path    string `json:"path"`
	Content string `json:"content"` // base64 for write
}

type Response struct {
	OK      bool     `json:"ok"`
	Exit    int      `json:"exit,omitempty"`
	Stdout  string   `json:"stdout,omitempty"`
	Stderr  string   `json:"stderr,omitempty"`
	Content string   `json:"content,omitempty"` // base64 for read
	Entries []string `json:"entries,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func main() {
	setup()
	ln, err := vsock.Listen(port, nil)
	if err != nil {
		logf("vsock listen: %v", err)
		os.Exit(1)
	}
	logf("sandbox-agent ready on vsock port %d", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			logf("accept: %v", err)
			continue
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, 1<<20)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "bad request: " + err.Error()})
		return
	}
	var resp Response
	switch req.Op {
	case "ping":
		resp = Response{OK: true}
	case "exec":
		resp = doExec(req.Command)
	case "read":
		data, err := os.ReadFile(req.Path)
		if err != nil {
			resp = Response{OK: false, Error: err.Error()}
		} else {
			resp = Response{OK: true, Content: base64.StdEncoding.EncodeToString(data)}
		}
	case "write":
		data, err := base64.StdEncoding.DecodeString(req.Content)
		if err != nil {
			resp = Response{OK: false, Error: "bad base64"}
		} else if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
			resp = Response{OK: false, Error: err.Error()}
		} else if err := os.WriteFile(req.Path, data, 0o644); err != nil {
			resp = Response{OK: false, Error: err.Error()}
		} else {
			resp = Response{OK: true}
		}
	case "list":
		entries, err := os.ReadDir(req.Path)
		if err != nil {
			resp = Response{OK: false, Error: err.Error()}
		} else {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			resp = Response{OK: true, Entries: names}
		}
	default:
		resp = Response{OK: false, Error: "unknown op " + req.Op}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func doExec(command string) Response {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = "/repo"
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return Response{OK: false, Error: err.Error()}
		}
	}
	return Response{OK: true, Exit: exit, Stdout: stdout.String(), Stderr: stderr.String()}
}

// setup mounts the virtual filesystems the tooling expects and creates the
// workspace. PID 1 in a microVM has no init system, so this is it.
func setup() {
	mustMount("proc", "proc", "/proc")
	mustMount("sysfs", "sysfs", "/sys")
	mustMount("devtmpfs", "devtmpfs", "/dev")
	mustMount("tmpfs", "tmpfs", "/tmp")
	_ = os.MkdirAll("/repo", 0o755)
	_ = os.MkdirAll("/tmp/home", 0o755)
	_ = os.MkdirAll("/tmp/gocache", 0o755)
	// PID 1 inherits a minimal environment; make the toolchain resolvable.
	os.Setenv("PATH", "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin")
	os.Setenv("HOME", "/tmp/home")
	os.Setenv("GOCACHE", "/tmp/gocache")
}

func mustMount(dev, fstype, target string) {
	_ = os.MkdirAll(target, 0o755)
	if out, err := exec.Command("/bin/mount", "-t", fstype, dev, target).CombinedOutput(); err != nil {
		logf("mount %s: %s", target, out)
	}
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "[sandbox-agent] "+format+"\n", args...)
}
