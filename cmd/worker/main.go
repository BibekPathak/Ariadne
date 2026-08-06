package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"adriane/internal/config"
	"adriane/internal/events"
	"adriane/internal/runtime"
	"adriane/internal/store"
	"adriane/internal/transport"
	"adriane/internal/workflow"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	bus, err := events.NewNATSBus(cfg.NATSURL)
	if err != nil {
		logger.Error("connect to nats", "err", err)
		os.Exit(1)
	}
	defer bus.Close()

	stack, err := runtime.Build(cfg, st, bus, logger)
	if err != nil {
		logger.Error("build worker stack", "err", err)
		os.Exit(1)
	}

	nc, err := nats.Connect(cfg.NATSURL, nats.Timeout(5*time.Second))
	if err != nil {
		logger.Error("connect to nats", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	workerID := "worker-" + randHex(6)
	stack.Worker.SetWorkerID(workerID)
	logger.Info("worker online", "id", workerID, "nats", cfg.NATSURL)

	// Heartbeats keep the control plane's dead-worker detection honest.
	go func() {
		tick := time.NewTicker(transport.HeartbeatInterval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				raw, _ := transport.Encode(&transport.HeartbeatMessage{WorkerID: workerID, TS: time.Now().UTC()})
				_ = nc.Publish(transport.SubjectHeartbeat, raw)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Claim tasks dispatched to the shared queue group.
	if _, err := nc.QueueSubscribe(transport.SubjectDispatch, transport.DispatchQueue, func(m *nats.Msg) {
		var task transport.TaskMessage
		if err := transport.Decode(m.Data, &task); err != nil {
			logger.Warn("ignoring malformed dispatch", "err", err)
			return
		}

		claim, _ := transport.Encode(&transport.ClaimMessage{TaskID: task.TaskID, WorkerID: workerID, Attempt: task.Attempt})
		_ = nc.Publish(transport.SubjectClaim, claim)

		logger.Info("claimed task", "worker", workerID, "task", task.TaskID, "type", task.Type)

		node := &workflow.Node{
			ID: task.TaskID, AgentID: task.AgentID, Name: task.Name,
			Template: task.Type, Inputs: task.Inputs, MaxAttempt: task.MaxAttempt,
		}
		outputs, runErr := stack.Worker.Run(ctx, node)

		res := &transport.ResultMessage{TaskID: task.TaskID, WorkerID: workerID, Attempt: task.Attempt, Outputs: outputs}
		if runErr != nil {
			res.Error = runErr.Error()
		}
		raw, _ := transport.Encode(res)
		if err := nc.Publish(transport.SubjectResult, raw); err != nil {
			logger.Error("publish result", "task", task.TaskID, "err", err)
		}
		logger.Info("finished task", "worker", workerID, "task", task.TaskID, "err", runErr != nil)
	}); err != nil {
		logger.Error("subscribe to dispatch", "err", err)
		os.Exit(1)
	}

	<-ctx.Done()
	logger.Info("worker shutting down", "id", workerID)
}

func randHex(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
