package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"adriane/internal/events"
	"adriane/internal/obs"
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
// retries on failure and monitors health. Workers are stateless, so the pool
// is modelled as a set of execution slots; Phase 4 replaces the in-process
// slots with a NATS-backed pool of remote workers.
type Scheduler struct {
	bus       events.EventBus
	logger    *slog.Logger
	worker    Worker
	metrics   *obs.Metrics
	mu        sync.Mutex
	workers   map[string]*workerRecord
	currentID string
	depth     atomic.Int64
}

func NewScheduler(bus events.EventBus, worker Worker, logger *slog.Logger, size int, metrics *obs.Metrics) *Scheduler {
	if size <= 0 {
		size = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Scheduler{bus: bus, logger: logger, worker: worker, metrics: metrics, workers: map[string]*workerRecord{}}
	for i := 1; i <= size; i++ {
		s.registerWorker(fmt.Sprintf("worker-%d", i))
	}
	return s
}

func (s *Scheduler) registerWorker(id string) {
	s.workers[id] = &workerRecord{id: id, state: WorkerRegistered}
}

// Depth is the number of tasks currently in execution (used by the autoscaler).
func (s *Scheduler) Depth() int64 { return s.depth.Load() }

// Execute implements workflow.TaskExecutor.
func (s *Scheduler) Execute(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	s.depth.Add(1)
	defer s.depth.Add(-1)
	if s.metrics != nil {
		s.metrics.QueueDepth.Inc()
		defer s.metrics.QueueDepth.Dec()
	}
	rec, err := s.reserve()
	if err != nil {
		return nil, err
	}
	defer func() {
		rec.state = WorkerCompleted
		rec.state = WorkerIdle
		s.updateUtilization()
	}()

	var lastErr error
	for attempt := 1; attempt <= node.MaxAttempt; attempt++ {
		if attempt > 1 {
			if s.metrics != nil {
				s.metrics.Retries.Inc()
			}
			_ = s.bus.Publish(ctx, events.New(node.AgentID, node.ID, events.RetryScheduled, map[string]any{
				"attempt": attempt,
				"max":     node.MaxAttempt,
				"error":   lastErr.Error(),
			}))
			s.logger.Warn("retrying task", "task", node.ID, "attempt", attempt, "err", lastErr)
		}
		rec.state = WorkerRunning
		s.updateUtilization()
		outputs, err := s.worker.Run(ctx, node)
		if err == nil {
			return outputs, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("task %s failed after %d attempts: %w", node.ID, node.MaxAttempt, lastErr)
}

// updateUtilization publishes the fraction of worker slots currently running.
func (s *Scheduler) updateUtilization() {
	if s.metrics == nil {
		return
	}
	s.mu.Lock()
	total := len(s.workers)
	running := 0
	for _, w := range s.workers {
		if w.state == WorkerRunning {
			running++
		}
	}
	s.mu.Unlock()
	if total > 0 {
		s.metrics.WorkerUtilization.WithLabelValues("running").Set(float64(running) / float64(total))
	}
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
