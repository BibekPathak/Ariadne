package scheduler

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"adriane/internal/transport"
	"adriane/internal/workflow"
)

func startTestNATS(t *testing.T) string {
	t.Helper()
	opts := &server.Options{Host: "127.0.0.1", Port: -1, JetStream: false, NoLog: true}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(func() { srv.Shutdown() })
	return srv.ClientURL()
}

func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url, nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// fakeWorker simulates a remote worker: claims a dispatch, runs a fake task,
// and publishes a result.
func natsFakeWorker(t *testing.T, url string, delay time.Duration, fail bool) {
	t.Helper()
	nc := connect(t, url)
	if _, err := nc.Subscribe(transport.SubjectDispatch, func(m *nats.Msg) {
		var task transport.TaskMessage
		if err := transport.Decode(m.Data, &task); err != nil {
			return
		}
		claim, _ := transport.Encode(&transport.ClaimMessage{TaskID: task.TaskID, WorkerID: "test-worker", Attempt: task.Attempt})
		_ = nc.Publish(transport.SubjectClaim, claim)
		time.Sleep(delay)
		res := &transport.ResultMessage{TaskID: task.TaskID, WorkerID: "test-worker", Attempt: task.Attempt, Outputs: map[string]any{"node": task.Name}}
		if fail {
			res.Error = "boom"
		}
		raw, _ := transport.Encode(res)
		_ = nc.Publish(transport.SubjectResult, raw)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteDispatcherRun(t *testing.T) {
	url := startTestNATS(t)
	natsFakeWorker(t, url, 50*time.Millisecond, false)

	d, err := NewRemoteDispatcher(url, time.Minute, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	node := &workflow.Node{ID: "a_r1_analyze", AgentID: "a", Name: "analyze", Template: "analyze", Inputs: map[string]any{"goal": "x"}}
	out, err := d.Run(context.Background(), node)
	if err != nil {
		t.Fatal(err)
	}
	if out["node"] != "analyze" {
		t.Fatalf("unexpected outputs %v", out)
	}
}

func TestRemoteDispatcherSurfacesWorkerError(t *testing.T) {
	url := startTestNATS(t)
	natsFakeWorker(t, url, 0, true)

	d, _ := NewRemoteDispatcher(url, time.Minute, slog.New(slog.DiscardHandler))
	defer d.Close()

	node := &workflow.Node{ID: "a_t", AgentID: "a", Name: "t", Template: "analyze"}
	if _, err := d.Run(context.Background(), node); err == nil {
		t.Fatal("expected the worker's error to propagate")
	}
}

// deadWorker claims the task and then dies (never publishes a result). The
// dispatcher's janitor must abort the wait so the scheduler can reassign.
func natsDeadWorker(t *testing.T, url string) {
	t.Helper()
	nc := connect(t, url)
	// Keep the control plane's worker map warm with heartbeats for a moment,
	// then go silent.
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for i := 0; i < 2; i++ {
			<-tick.C
			hb, _ := transport.Encode(&transport.HeartbeatMessage{WorkerID: "dead-worker", TS: time.Now().UTC()})
			_ = nc.Publish(transport.SubjectHeartbeat, hb)
		}
		close(stop)
	}()
	<-stop
	if _, err := nc.Subscribe(transport.SubjectDispatch, func(m *nats.Msg) {
		var task transport.TaskMessage
		_ = transport.Decode(m.Data, &task)
		claim, _ := transport.Encode(&transport.ClaimMessage{TaskID: task.TaskID, WorkerID: "dead-worker", Attempt: task.Attempt})
		_ = nc.Publish(transport.SubjectClaim, claim)
		// never publish a result — worker died mid-task
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteDispatcherReassignsOnWorkerDeath(t *testing.T) {
	url := startTestNATS(t)
	natsDeadWorker(t, url)

	d, err := NewRemoteDispatcher(url, time.Minute, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.deadTimeout = 400 * time.Millisecond

	node := &workflow.Node{ID: "a_t", AgentID: "a", Name: "t", Template: "analyze"}
	_, err = d.Run(context.Background(), node)
	if err == nil {
		t.Fatal("expected reassignment error when the worker dies mid-task")
	}
}
