package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"adriane/internal/llm"
	"adriane/internal/sandbox"
)

// ExecContext is what a tool needs to operate: an isolated session plus the
// task's working directory inside it.
type ExecContext struct {
	Session sandbox.Session
	Workdir string
}

// Tool is a capability exposed to the agent. Every tool exposes
// Execute(input) -> output; the agent never sees implementation details.
type Tool interface {
	Name() string
	Description() string
	// Parameters returns a JSON schema fragment for the LLM.
	Parameters() map[string]any
	Execute(ctx context.Context, input map[string]any, ec *ExecContext) (string, error)
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: make(map[string]Tool)}
	for _, t := range ts {
		r.tools[t.Name()] = t
	}
	return r
}

func (r *Registry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Definitions returns the LLM tool schema for the named tools (or all tools
// if names is empty). Unknown names are skipped.
func (r *Registry) Definitions(names []string) []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	selected := map[string]bool{}
	for _, n := range names {
		selected[n] = true
	}
	keys := make([]string, 0, len(r.tools))
	for k := range r.tools {
		if len(names) == 0 || selected[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []llm.Tool
	for _, k := range keys {
		t := r.tools[k]
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}

func (r *Registry) Execute(ctx context.Context, name string, input map[string]any, ec *ExecContext) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return t.Execute(ctx, input, ec)
}
