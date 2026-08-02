package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"kubeai/internal/events"
	"kubeai/internal/store"
)

// NodeStore is the persistence seam used by the engine.
type NodeStore interface {
	Create(ctx context.Context, t *store.Task) error
	UpdateStatus(ctx context.Context, id, status, errMsg string) error
	UpdateOutputs(ctx context.Context, id string, outputs map[string]any) error
	IncAttempt(ctx context.Context, id string) error
}

// Engine walks the compiled DAG, dispatching ready nodes to the executor.
// Nodes without dependencies run concurrently; downstream nodes wait. Phase 1
// executes in-process; the same engine drives distributed workers in Phase 4.
type Engine struct {
	store   NodeStore
	bus     events.EventBus
	logger  *slog.Logger
	workers int
}

func NewEngine(ns NodeStore, bus events.EventBus, logger *slog.Logger, workers int) *Engine {
	if workers <= 0 {
		workers = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{store: ns, bus: bus, logger: logger, workers: workers}
}

func (e *Engine) Run(ctx context.Context, dag *DAG, executor TaskExecutor) error {
	for _, n := range dag.Nodes {
		if err := e.store.Create(ctx, toTask(n)); err != nil {
			return fmt.Errorf("persist task %s: %w", n.ID, err)
		}
	}

	sem := make(chan struct{}, e.workers)
	var wg sync.WaitGroup
	for dag.RemainingPending() && !dag.AnyFailed() {
		ready := dag.Ready()
		if len(ready) == 0 {
			// Nothing is ready and not everything is done: some dependency is
			// failed or blocked.
			dag.Blocked()
			break
		}
		for _, n := range ready {
			n.Status = StatusRunning
			if err := e.store.UpdateStatus(ctx, n.ID, string(StatusRunning), ""); err != nil {
				return err
			}
			wg.Add(1)
			go func(node *Node) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				e.executeNode(ctx, node, executor)
			}(n)
		}
		wg.Wait()
	}
	dag.Blocked()
	if dag.AnyFailed() {
		return fmt.Errorf("workflow failed: one or more tasks could not complete")
	}
	return nil
}

func (e *Engine) executeNode(ctx context.Context, n *Node, executor TaskExecutor) {
	if err := e.store.IncAttempt(ctx, n.ID); err != nil {
		e.logger.Error("increment attempt", "task", n.ID, "err", err)
	}
	n.Attempt++

	outputs, err := executor.Execute(ctx, n)
	if err != nil {
		n.Status = StatusFailed
		n.Error = err.Error()
		if eerr := e.store.UpdateStatus(ctx, n.ID, string(StatusFailed), n.Error); eerr != nil {
			e.logger.Error("persist task failure", "task", n.ID, "err", eerr)
		}
		e.logger.Error("task failed", "task", n.ID, "err", err)
		return
	}
	n.Status = StatusDone
	n.Outputs = outputs
	if eerr := e.store.UpdateOutputs(ctx, n.ID, outputs); eerr != nil {
		e.logger.Error("persist outputs", "task", n.ID, "err", eerr)
	}
	if eerr := e.store.UpdateStatus(ctx, n.ID, string(StatusDone), ""); eerr != nil {
		e.logger.Error("persist task status", "task", n.ID, "err", eerr)
	}
}

func toTask(n *Node) *store.Task {
	return &store.Task{
		ID:         n.ID,
		AgentID:    n.AgentID,
		Name:       n.Name,
		Template:   n.Template,
		Status:     string(n.Status),
		DependsOn:  n.DependsOn,
		Inputs:     n.Inputs,
		Outputs:    n.Outputs,
		Attempt:    n.Attempt,
		MaxAttempt: n.MaxAttempt,
		Error:      n.Error,
	}
}
