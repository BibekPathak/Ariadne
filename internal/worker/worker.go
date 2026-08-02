package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"adriane/internal/artifacts"
	ctxbuilder "adriane/internal/context"
	"adriane/internal/events"
	"adriane/internal/llm"
	"adriane/internal/memory"
	"adriane/internal/sandbox"
	"adriane/internal/tasks"
	"adriane/internal/tools"
	"adriane/internal/workflow"
)

type Config struct {
	MaxIterations int
	RepoBaseDir   string
	Sandbox       sandbox.Sandbox
	Memory        memory.Memory // optional; nil disables memory
	UseSemantic   bool
}

// Worker executes a single node: it prepares an isolated sandbox, loads the
// repo, runs the agent loop (LLM + tools) and returns outputs. Workers are
// stateless: everything needed comes from the node inputs. Memory is
// per-agent, so it is read and written around each task.
type Worker struct {
	cfg         Config
	provider    llm.Provider
	model       string
	registry    *tools.Registry
	templates   *tasks.Registry
	context     *ctxbuilder.Builder
	artifacts   *artifacts.Store
	bus         events.Publisher
	logger      *slog.Logger
	memory      memory.Memory
	useSemantic bool
}

func New(cfg Config, provider llm.Provider, model string, registry *tools.Registry,
	templates *tasks.Registry, ctxBuilder *ctxbuilder.Builder, arts *artifacts.Store,
	bus events.Publisher, logger *slog.Logger) *Worker {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 25
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{cfg: cfg, provider: provider, model: model, registry: registry,
		templates: templates, context: ctxBuilder, artifacts: arts, bus: bus, logger: logger,
		memory: cfg.Memory, useSemantic: cfg.UseSemantic}
}

func (w *Worker) Run(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	tpl, ok := w.templates.Get(node.Template)
	if !ok {
		return nil, fmt.Errorf("worker: unknown task template %q", node.Template)
	}

	if err := w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.TaskStarted, map[string]any{
		"name": node.Name, "template": node.Template,
	})); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, tpl.Timeout)
	defer cancel()

	repoHost, cleanup, err := w.prepareRepo(ctx, node)
	if err != nil {
		_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.TaskFailed, map[string]any{"error": err.Error()}))
		return nil, err
	}
	defer cleanup()

	session, err := w.cfg.Sandbox.Prepare(ctx, &sandbox.RepoSource{LocalPath: repoHost})
	if err != nil {
		_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.TaskFailed, map[string]any{"error": err.Error()}))
		return nil, fmt.Errorf("prepare sandbox: %w", err)
	}
	defer func() { _ = session.Destroy(context.Background()) }()

	// Idempotent git identity so the agent can commit if asked to.
	_, _ = session.ExecShell(ctx, "git config --global user.email agent@kubeai.local && git config --global user.name kubeai-agent")

	recalled := w.loadMemory(ctx, node)

	outputs, transcript, err := w.runAgentLoop(ctx, node, tpl, session, recalled)
	w.saveTranscript(ctx, node, transcript)
	w.storeMemory(ctx, node, recalled, outputs, transcript, err)
	if err != nil {
		_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.TaskFailed, map[string]any{"error": err.Error()}))
		return nil, err
	}

	_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.TaskFinished, map[string]any{
		"name": node.Name, "outputs": outputs,
	}))
	return outputs, nil
}

// prepareRepo makes the repository available on the host so the sandbox can
// mount it. Remote repos are cloned; local paths are used directly.
func (w *Worker) prepareRepo(ctx context.Context, node *workflow.Node) (string, func(), error) {
	repoURL, _ := node.Inputs["repo_url"].(string)
	repoPath, _ := node.Inputs["repo_path"].(string)

	noop := func() {}
	if repoPath != "" {
		abs, err := filepath.Abs(repoPath)
		if err != nil {
			return "", noop, err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", noop, fmt.Errorf("repo_path %s not found: %w", abs, err)
		}
		return abs, noop, nil
	}

	dest := filepath.Join(w.cfg.RepoBaseDir, node.AgentID)
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", noop, err
	}
	if repoURL != "" {
		if err := gitClone(ctx, repoURL, dest); err != nil {
			return "", noop, fmt.Errorf("clone repo: %w", err)
		}
	}
	return dest, func() { _ = os.RemoveAll(dest) }, nil
}

func gitClone(ctx context.Context, url, dest string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", url, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

// runAgentLoop drives the LLM tool-calling loop inside the sandbox.
func (w *Worker) runAgentLoop(ctx context.Context, node *workflow.Node, tpl tasks.Template, session sandbox.Session, recalled []ctxbuilder.MemoryItem) (map[string]any, []llm.Message, error) {
	toolDefs := w.registry.Definitions(tpl.AllowedTools)

	goal, _ := node.Inputs["goal"].(string)
	systemPrompt := "You are a capable coding agent working inside an isolated sandbox. " +
		"Your workspace is /repo. Use the provided tools to complete the task. " +
		"Be concise and prefer small, focused steps."

	history := []llm.Message{}
	ec := &tools.ExecContext{Session: session, Workdir: "/repo"}
	transcript := []llm.Message{}

	var toolUseCount int
	for i := 0; i < w.cfg.MaxIterations; i++ {
		req := w.context.Build(ctxbuilder.Input{
			SystemPrompt: systemPrompt,
			Goal:         goal,
			TaskPrompt:   tpl.Prompt,
			Messages:     history,
			Memories:     recalled,
			MaxTokens:    16000,
		})
		req.Model = w.model
		req.Tools = toolDefs

		resp, err := w.provider.Generate(ctx, *req)
		if err != nil {
			return nil, transcript, fmt.Errorf("llm call: %w", err)
		}
		_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.LLMCalled, map[string]any{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		}))

		assistant := llm.Message{Role: llm.RoleAssistant, Content: resp.Content, ToolCalls: resp.ToolCalls}
		transcript = append(transcript, assistant)
		history = append(history, assistant)

		if len(resp.ToolCalls) == 0 {
			return map[string]any{
				"final_message": resp.Content,
				"tool_calls":    toolUseCount,
			}, transcript, nil
		}

		for _, tc := range resp.ToolCalls {
			toolUseCount++
			args, err := tools.MarshalInput(tc.Function.Arguments)
			if err != nil {
				return nil, transcript, err
			}
			_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.ToolCalled, map[string]any{
				"tool": tc.Function.Name, "args": args,
			}))
			result, err := w.registry.Execute(ctx, tc.Function.Name, args, ec)
			if err != nil {
				result = result + "\n[ERROR] " + err.Error()
			}
			_ = w.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.ToolFinished, map[string]any{
				"tool": tc.Function.Name, "error": err != nil,
			}))
			toolMsg := llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Name: tc.Function.Name, Content: result}
			transcript = append(transcript, toolMsg)
			history = append(history, toolMsg)
		}
	}
	return nil, transcript, fmt.Errorf("agent exceeded %d iterations", w.cfg.MaxIterations)
}

func (w *Worker) saveTranscript(ctx context.Context, node *workflow.Node, transcript []llm.Message) {
	if len(transcript) == 0 {
		return
	}
	raw, err := json.MarshalIndent(transcript, "", "  ")
	if err != nil {
		return
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	_, _ = w.artifacts.Save(ctx, node.AgentID, node.ID, "log", "transcript_"+ts+".json", raw)
}
