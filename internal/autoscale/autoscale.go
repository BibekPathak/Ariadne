// Package autoscale spawns and drains remote worker subprocesses based on
// scheduler queue depth.
package autoscale

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Manager owns cmd/worker subprocesses.
type Manager struct {
	binary string
	logger *slog.Logger

	mu    sync.Mutex
	procs []*exec.Cmd
}

func NewManager(binary string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{binary: binary, logger: logger}
}

// Spawn starts a new worker subprocess. The worker joins the NATS dispatch
// queue group and starts heartbeating on its own.
func (m *Manager) Spawn(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, m.binary)
	cmd.Env = os.Environ()
	cmd.Stdout = m.logWriter()
	cmd.Stderr = m.logWriter()
	if err := cmd.Start(); err != nil {
		return err
	}
	m.mu.Lock()
	m.procs = append(m.procs, cmd)
	m.mu.Unlock()
	m.logger.Info("autoscaler: spawned worker", "pid", cmd.Process.Pid)
	return nil
}

func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.procs)
}

// TerminateOne gracefully drains one worker: SIGTERM, then wait for it to
// finish its in-flight task and exit. Never SIGKILL.
func (m *Manager) TerminateOne(timeout time.Duration) bool {
	m.mu.Lock()
	if len(m.procs) == 0 {
		m.mu.Unlock()
		return false
	}
	cmd := m.procs[len(m.procs)-1]
	m.procs = m.procs[:len(m.procs)-1]
	m.mu.Unlock()

	m.logger.Info("autoscaler: draining worker", "pid", cmd.Process.Pid)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		return true
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		m.logger.Warn("autoscaler: drain timeout; worker still exiting", "pid", cmd.Process.Pid)
	}
	return true
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.procs {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

func (m *Manager) logWriter() io.Writer {
	return &logWriter{logger: m.logger}
}

type logWriter struct {
	logger *slog.Logger
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.logger.Info(string(p))
	return len(p), nil
}

// Autoscaler scales the worker pool from scheduler depth.
type Autoscaler struct {
	m           *Manager
	depth       func() int64
	min, max    int
	upThreshold int
	idle        time.Duration
	poll        time.Duration
	logger      *slog.Logger

	mu           sync.Mutex
	lastActivity time.Time
}

func NewAutoscaler(m *Manager, depth func() int64, min, max, upThreshold int, idle time.Duration, logger *slog.Logger) *Autoscaler {
	return NewAutoscalerPoll(m, depth, min, max, upThreshold, idle, time.Second, logger)
}

func NewAutoscalerPoll(m *Manager, depth func() int64, min, max, upThreshold int, idle, poll time.Duration, logger *slog.Logger) *Autoscaler {
	if max <= 0 {
		max = 8
	}
	if poll <= 0 {
		poll = time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Autoscaler{m: m, depth: depth, min: min, max: max, upThreshold: upThreshold, idle: idle, poll: poll, logger: logger}
}

func (a *Autoscaler) Run(ctx context.Context) {
	tick := time.NewTicker(a.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			a.m.StopAll()
			return
		case <-tick.C:
			a.tick()
		}
	}
}

func (a *Autoscaler) tick() {
	d := a.depth()
	count := a.m.Count()

	if d > 0 {
		a.mu.Lock()
		a.lastActivity = time.Now()
		a.mu.Unlock()
	}

	// Ensure the floor is always met so there is a worker to claim tasks.
	if count < a.min {
		_ = a.m.Spawn(context.Background())
		return
	}

	// Scale up when depth exceeds the threshold (allow room before saturation).
	if d > int64(a.upThreshold) && count < a.max {
		_ = a.m.Spawn(context.Background())
		return
	}

	// Scale down one idle worker at a time.
	if count > a.min && d == 0 {
		a.mu.Lock()
		idle := time.Since(a.lastActivity)
		a.mu.Unlock()
		if idle > a.idle {
			if a.m.TerminateOne(30 * time.Second) {
				a.logger.Info("autoscaler: scaled down", "workers", a.m.Count())
			}
		}
	}
}
