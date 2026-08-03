package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"adriane/internal/agents"
	"adriane/internal/config"
	"adriane/internal/events"
	"adriane/internal/planner"
	"adriane/internal/runtime"
	"adriane/internal/scheduler"
	"adriane/internal/store"
	"adriane/internal/workflow"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx := context.Background()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := store.Migrate(cfg.DatabaseURL); err != nil {
			logger.Error("migration failed", "err", err)
			os.Exit(1)
		}
		logger.Info("migrations applied")
		return
	}

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	bus := events.NewInMemoryBus(logger)
	bus.SetPersister(func(e events.Event) (int64, error) {
		return st.Events.Append(ctx, e)
	})
	defer bus.Close()

	// Remote mode: workers publish events over NATS; forward them into the
	// local bus so they are persisted and streamed to SSE like local events.
	if cfg.WorkerMode == "remote" {
		remoteBus, err := events.NewNATSBus(cfg.NATSURL)
		if err != nil {
			logger.Error("connect to nats", "err", err)
			os.Exit(1)
		}
		defer remoteBus.Close()
		go func() {
			if err := remoteBus.Forward(ctx, func(e events.Event) error {
				return bus.Publish(ctx, e)
			}); err != nil && ctx.Err() == nil {
				logger.Error("nats event forwarder stopped", "err", err)
			}
		}()
	}

	service := wire(ctx, cfg, st, bus, logger)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           routes(service, bus, cfg.Timeout, logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("control plane listening", "addr", cfg.HTTPAddr, "worker_mode", cfg.WorkerMode)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

// wire assembles the control plane. In embedded mode the scheduler dispatches
// to an in-process worker; in remote mode it dispatches over NATS to a pool of
// standalone workers.
func wire(ctx context.Context, cfg config.Config, st *store.Store, bus events.EventBus, logger *slog.Logger) *agents.AgentService {
	stack, err := runtime.Build(cfg, st, bus, logger)
	if err != nil {
		logger.Error("build runtime", "err", err)
		os.Exit(1)
	}

	agentTemplates := agents.NewTemplateRegistry()

	var plannerIf planner.Planner
	if cfg.RouterPrimaryKey == "" {
		plannerIf = planner.StaticPlanner{}
	} else {
		plannerIf = planner.NewLLMPlanner(stack.Router, stack.TaskTemplates)
	}

	var workerIf scheduler.Worker = stack.Worker
	if cfg.WorkerMode == "remote" {
		dispatcher, err := scheduler.NewRemoteDispatcher(cfg.NATSURL, cfg.Timeout, logger)
		if err != nil {
			logger.Error("init remote dispatcher", "err", err)
			os.Exit(1)
		}
		workerIf = dispatcher
	}

	sched := scheduler.NewScheduler(bus, workerIf, logger, cfg.EngineConcurrency)
	compiler := workflow.NewCompiler(stack.TaskTemplates)
	engine := workflow.NewEngine(st.Tasks, bus, logger, cfg.EngineConcurrency)
	return agents.NewAgentService(st, bus, agentTemplates, plannerIf, compiler, engine, sched, logger)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
