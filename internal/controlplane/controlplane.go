// Package controlplane assembles the full control plane: store, event bus
// (with NATS forwarding in remote mode), the runtime stack, the planner,
// compiler, engine and scheduler, and the agent service. Both the API gateway
// and the eval CLI build the same assembly, so agent runs behave identically
// in both contexts.
package controlplane

import (
	"context"
	"log/slog"

	"adriane/internal/agents"
	"adriane/internal/config"
	"adriane/internal/events"
	"adriane/internal/planner"
	"adriane/internal/runtime"
	"adriane/internal/scheduler"
	"adriane/internal/store"
	"adriane/internal/workflow"
)

type ControlPlane struct {
	Store  *store.Store
	Bus    events.EventBus
	Stack  *runtime.Stack
	Agent  *agents.AgentService
	Logger *slog.Logger

	remoteBus *events.NATSBus
}

func Build(ctx context.Context, cfg config.Config, logger *slog.Logger) (*ControlPlane, error) {
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	bus := events.NewInMemoryBus(logger)
	bus.SetPersister(func(e events.Event) (int64, error) {
		return st.Events.Append(ctx, e)
	})

	var remoteBus *events.NATSBus
	if cfg.WorkerMode == "remote" {
		remoteBus, err = events.NewNATSBus(cfg.NATSURL)
		if err != nil {
			st.Close()
			bus.Close()
			return nil, err
		}
		go func() {
			if err := remoteBus.Forward(ctx, func(e events.Event) error {
				return bus.Publish(ctx, e)
			}); err != nil && ctx.Err() == nil {
				logger.Error("nats event forwarder stopped", "err", err)
			}
		}()
	}

	stack, err := runtime.Build(cfg, st, bus, logger)
	if err != nil {
		st.Close()
		bus.Close()
		return nil, err
	}

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
			st.Close()
			bus.Close()
			return nil, err
		}
		workerIf = dispatcher
	}

	sched := scheduler.NewScheduler(bus, workerIf, logger, cfg.EngineConcurrency, stack.Metrics)
	compiler := workflow.NewCompiler(stack.TaskTemplates)
	engine := workflow.NewEngine(st.Tasks, bus, logger, cfg.EngineConcurrency, stack.Metrics)
	agentService := agents.NewAgentService(st, bus, agents.NewTemplateRegistry(), plannerIf, compiler, engine, sched, stack.Metrics, logger)

	return &ControlPlane{
		Store: st, Bus: bus, Stack: stack, Agent: agentService, Logger: logger, remoteBus: remoteBus,
	}, nil
}

func (c *ControlPlane) Close() {
	if c.remoteBus != nil {
		_ = c.remoteBus.Close()
	}
	_ = c.Bus.Close()
	c.Store.Close()
}
