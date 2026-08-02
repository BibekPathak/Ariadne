package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"adriane/internal/events"
	"adriane/internal/workflow"
)

// Worker is the executor abstraction the scheduler dispatches to. Phase 1 has
// a single in-process worker; Phase 4 replaces this with a NATS-backed pool.
type Worker interface {
	// Run executes a single node and returns its outputs.
	Run(ctx context.Context, node *workflow.Node) (map[string]any, error)
}

// WorkerState is the lifecycle every worker tracks.
type WorkerState string

const (
	WorkerRegistered WorkerState = "registered"
	WorkerIdle       WorkerState = "idle"
	WorkerReserved   WorkerState = "reserved"
	WorkerRunning    WorkerState = "running"
	WorkerCompleted  WorkerState = "completed"
)

type workerRecord struct {
	id    string
	state WorkerState
}

// Scheduler finds an available worker, reserves it, dispatches the task,
// retries on failure and monitors health. Phase 1 tracks a single worker;
// the bookkeeping mirrors what the distributed scheduler enforces in Phase 4.
type Scheduler struct {
	bus       events.EventBus
	logger    *slog.Logger
	worker    Worker
	mu        sync.Mutex
	workers   map[string]*workerRecord
	currentID string
}

func NewScheduler(bus events.EventBus, worker Worker, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Scheduler{bus: bus, logger: logger, worker: worker, workers: map[string]*workerRecord{}}
	s.registerWorker("worker-1")
	return s
}

func (s *Scheduler) registerWorker(id string) {
	s.workers[id] = &workerRecord{id: id, state: WorkerRegistered}
}

// Execute implements workflow.TaskExecutor.
func (s *Scheduler) Execute(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	rec, err := s.reserve()
	if err != nil {
		return nil, err
	}
	defer func() {
		rec.state = WorkerCompleted
		rec.state = WorkerIdle
	}()

	var lastErr error
	for attempt := 1; attempt <= node.MaxAttempt; attempt++ {
		if attempt > 1 {
			_ = s.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.RetryScheduled, map[string]any{
				"attempt": attempt,
				"max":     node.MaxAttempt,
				"error":   lastErr.Error(),
			}))
			s.logger.Warn("retrying task", "task", node.ID, "attempt", attempt, "err", lastErr)
		}
		rec.state = WorkerRunning
		outputs, err := s.worker.Run(ctx, node)
		if err == nil {
			return outputs, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("task %s failed after %d attempts: %w", node.ID, node.MaxAttempt, lastErr)
}

func (s *Scheduler) reserve() (*workerRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.workers {
		if w.state == WorkerIdle || w.state == WorkerRegistered || w.state == WorkerCompleted {
			w.state = WorkerReserved
			return w, nil
		}
	}
	return nil, fmt.Errorf("no available worker")
}
