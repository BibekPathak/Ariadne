package llm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// ScriptedProvider is a deterministic fake for tests and offline demos.
// It lets callers control the exact behaviour: it returns queued responses
// one by one. If a response has ToolCalls, the caller is expected to append
// a tool result message and call again, at which point the next response is
// returned. If the queue is exhausted it returns a plain final message.
type ScriptedProvider struct {
	name     string
	mu       sync.Mutex
	queue    []Response
	finished atomic.Bool
}

func NewScriptedProvider(name string, responses ...Response) *ScriptedProvider {
	return &ScriptedProvider{name: name, queue: responses}
}

func (p *ScriptedProvider) Name() string { return p.name }

func (p *ScriptedProvider) Generate(ctx context.Context, req Request) (*Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) > 0 {
		r := p.queue[0]
		p.queue = p.queue[1:]
		return &r, nil
	}
	if p.finished.Load() {
		return nil, fmt.Errorf("scripted provider exhausted")
	}
	p.finished.Store(true)
	return &Response{Content: "Done."}, nil
}
