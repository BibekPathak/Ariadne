package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"adriane/internal/transport"
	"adriane/internal/workflow"
)

// RemoteDispatcher implements Worker by dispatching TaskMessages over NATS and
// waiting for the matching result. It tracks worker heartbeats and claims so a
// dead worker's in-flight tasks are aborted promptly and reassigned (the
// scheduler retries by dispatching again to the queue group).
type RemoteDispatcher struct {
	nc     *nats.Conn
	logger *slog.Logger
	wait   time.Duration

	mu          sync.Mutex
	waiters     map[string]chan *transport.ResultMessage
	abort       map[string]chan struct{}
	taskWorker  map[string]string // dispatch key -> worker id
	workers     map[string]time.Time
	dispatchSeq map[string]int

	deadTimeout time.Duration
}

func NewRemoteDispatcher(url string, wait time.Duration, logger *slog.Logger) (*RemoteDispatcher, error) {
	nc, err := nats.Connect(url, nats.Timeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	if wait <= 0 {
		wait = 30 * time.Minute
	}
	d := &RemoteDispatcher{
		nc: nc, logger: logger, wait: wait,
		waiters:     map[string]chan *transport.ResultMessage{},
		abort:       map[string]chan struct{}{},
		taskWorker:  map[string]string{},
		workers:     map[string]time.Time{},
		dispatchSeq: map[string]int{},
		deadTimeout: 6 * time.Second,
	}

	if _, err := nc.Subscribe(transport.SubjectResult, d.onResult); err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe results: %w", err)
	}
	if _, err := nc.Subscribe(transport.SubjectClaim, d.onClaim); err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe claims: %w", err)
	}
	if _, err := nc.Subscribe(transport.SubjectHeartbeat, d.onHeartbeat); err != nil {
		nc.Close()
		return nil, fmt.Errorf("subscribe heartbeats: %w", err)
	}
	go d.janitor()
	return d, nil
}

func (d *RemoteDispatcher) onResult(m *nats.Msg) {
	var r transport.ResultMessage
	if err := transport.Decode(m.Data, &r); err != nil {
		return
	}
	key := dispatchKey(r.TaskID, r.Attempt)
	d.mu.Lock()
	ch := d.waiters[key]
	d.mu.Unlock()
	if ch != nil {
		select {
		case ch <- &r:
		default:
		}
	}
}

func (d *RemoteDispatcher) onClaim(m *nats.Msg) {
	var c transport.ClaimMessage
	if err := transport.Decode(m.Data, &c); err != nil {
		return
	}
	d.mu.Lock()
	d.taskWorker[dispatchKey(c.TaskID, c.Attempt)] = c.WorkerID
	d.workers[c.WorkerID] = time.Now()
	d.mu.Unlock()
}

func (d *RemoteDispatcher) onHeartbeat(m *nats.Msg) {
	var h transport.HeartbeatMessage
	if err := transport.Decode(m.Data, &h); err != nil {
		return
	}
	d.mu.Lock()
	d.workers[h.WorkerID] = time.Now()
	d.mu.Unlock()
}

// Run implements Worker: dispatch, wait, and surface reassignment on death.
func (d *RemoteDispatcher) Run(ctx context.Context, node *workflow.Node) (map[string]any, error) {
	d.mu.Lock()
	d.dispatchSeq[node.ID]++
	attempt := d.dispatchSeq[node.ID]
	key := dispatchKey(node.ID, attempt)
	resultCh := make(chan *transport.ResultMessage, 1)
	abortCh := make(chan struct{})
	d.waiters[key] = resultCh
	d.abort[key] = abortCh
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.waiters, key)
		delete(d.abort, key)
		delete(d.taskWorker, key)
		d.mu.Unlock()
	}()

	task := &transport.TaskMessage{
		TaskID: node.ID, WorkflowID: node.AgentID, AgentID: node.AgentID,
		Name: node.Name, Type: node.Template, Inputs: node.Inputs,
		Attempt: attempt, MaxAttempt: node.MaxAttempt,
		Deadline: time.Now().Add(d.wait), TraceID: node.ID,
	}
	raw, err := transport.Encode(task)
	if err != nil {
		return nil, err
	}
	if err := d.nc.Publish(transport.SubjectDispatch, raw); err != nil {
		return nil, fmt.Errorf("publish dispatch: %w", err)
	}

	timer := time.NewTimer(d.wait)
	defer timer.Stop()
	select {
	case r := <-resultCh:
		if r.Error != "" {
			return nil, fmt.Errorf("remote task %s failed: %s", node.ID, r.Error)
		}
		return r.Outputs, nil
	case <-abortCh:
		return nil, fmt.Errorf("worker died while running task %s; reassigning", node.ID)
	case <-timer.C:
		return nil, fmt.Errorf("task %s timed out awaiting result", node.ID)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// janitor aborts in-flight tasks owned by workers whose heartbeats have lapsed.
func (d *RemoteDispatcher) janitor() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		d.mu.Lock()
		var dead []string
		for id, last := range d.workers {
			if now.Sub(last) > d.deadTimeout {
				dead = append(dead, id)
			}
		}
		for _, id := range dead {
			delete(d.workers, id)
			for key, wid := range d.taskWorker {
				if wid == id {
					if ch, ok := d.abort[key]; ok {
						close(ch)
					}
					delete(d.taskWorker, key)
				}
			}
			d.logger.Warn("worker marked dead; reassigning its tasks", "worker", id)
		}
		d.mu.Unlock()
	}
}

func (d *RemoteDispatcher) Close() { d.nc.Close() }

func dispatchKey(taskID string, attempt int) string {
	return fmt.Sprintf("%s#%d", taskID, attempt)
}
