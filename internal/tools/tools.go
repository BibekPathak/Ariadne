package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func strArg(input map[string]any, key string) string {
	if v, ok := input[key].(string); ok {
		return v
	}
	return ""
}

// ReadFileTool exposes reading files inside the sandbox.
type ReadFileTool struct{}

func (ReadFileTool) Name() string        { return "read_file" }
func (ReadFileTool) Description() string { return "Read the contents of a file in the workspace." }
func (ReadFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Path relative to the workspace root"},
		},
		"required": []string{"path"},
	}
}
func (ReadFileTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	path := filepath.Join(ec.Workdir, strArg(input, "path"))
	data, err := ec.Session.ReadFile(ctx, path)
	if err != nil {
		return "", err
	}
	if len(data) > 100_000 {
		return fmt.Sprintf("[file truncated, %d bytes total]\n%s", len(data), string(data[:100_000])), nil
	}
	return string(data), nil
}

// WriteFileTool writes files inside the sandbox.
type WriteFileTool struct{}

func (WriteFileTool) Name() string        { return "write_file" }
func (WriteFileTool) Description() string { return "Write or overwrite a file in the workspace." }
func (WriteFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "Path relative to the workspace root"},
			"content": map[string]any{"type": "string", "description": "Full file content"},
		},
		"required": []string{"path", "content"},
	}
}
func (WriteFileTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	path := filepath.Join(ec.Workdir, strArg(input, "path"))
	content := strArg(input, "content")
	if err := ec.Session.WriteFile(ctx, path, []byte(content)); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

// ListFilesTool lists a directory in the sandbox.
type ListFilesTool struct{}

func (ListFilesTool) Name() string        { return "list_files" }
func (ListFilesTool) Description() string { return "List files in a directory of the workspace." }
func (ListFilesTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "Directory path relative to the workspace root"},
		},
	}
}
func (ListFilesTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	dir := filepath.Join(ec.Workdir, strArg(input, "path"))
	files, err := ec.Session.ListFiles(ctx, dir)
	if err != nil {
		return "", err
	}
	return strings.Join(files, "\n"), nil
}

// ShellTool runs shell commands inside the sandbox.
type ShellTool struct{}

func (ShellTool) Name() string        { return "shell" }
func (ShellTool) Description() string { return "Run a shell command in the workspace." }
func (ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command to run"},
		},
		"required": []string{"command"},
	}
}
func (ShellTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	cmd := strArg(input, "command")
	if ec.Workdir != "" {
		cmd = "cd " + ec.Workdir + " && " + cmd
	}
	out, err := ec.Session.ExecShell(ctx, cmd)
	res := out.Stdout
	if out.Stderr != "" {
		res += "\n[stderr]\n" + out.Stderr
	}
	if len(res) > 50_000 {
		res = res[:50_000] + "\n[output truncated]"
	}
	res += fmt.Sprintf("\n[exit code %d]", out.ExitCode)
	if err != nil {
		return res, fmt.Errorf("shell command failed: %w", err)
	}
	return res, nil
}

// GitTool exposes git operations inside the sandbox.
type GitTool struct{}

func (GitTool) Name() string        { return "git" }
func (GitTool) Description() string { return "Run a git command in the repository." }
func (GitTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{"type": "string", "description": "Git arguments, e.g. \"status\" or \"diff\""},
		},
		"required": []string{"args"},
	}
}
func (GitTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	args := strArg(input, "args")
	out, err := ec.Session.ExecShell(ctx, "git "+args)
	res := out.Stdout
	if out.Stderr != "" {
		res += "\n[stderr]\n" + out.Stderr
	}
	if len(res) > 50_000 {
		res = res[:50_000] + "\n[output truncated]"
	}
	return res, err
}

// HTTPGetTool fetches a URL. Phase 1 runs on the host for simplicity; a
// sandboxed network proxy is planned for the hardening phase.
type HTTPGetTool struct {
	Client *http.Client
}

func (t HTTPGetTool) Name() string        { return "http_get" }
func (t HTTPGetTool) Description() string { return "Fetch a URL and return its body." }
func (t HTTPGetTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "description": "URL to fetch"},
		},
		"required": []string{"url"},
	}
}
func (t HTTPGetTool) Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error) {
	url := strArg(input, "url")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("status %d\n%s", resp.StatusCode, body), nil
}

// MarshalInput converts a JSON string (as provided by the LLM) into the
// argument map used by Execute.
func MarshalInput(raw string) (map[string]any, error) {
	in := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return in, nil
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	return in, nil
}
