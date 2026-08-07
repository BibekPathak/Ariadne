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
	"adriane/internal/auth"
	"adriane/internal/autoscale"
	"adriane/internal/config"
	"adriane/internal/events"
	"adriane/internal/leader"
	"adriane/internal/planner"
	"adriane/internal/quota"
	"adriane/internal/ratelimit"
	"adriane/internal/runtime"
	"adriane/internal/scheduler"
	"adriane/internal/store"
	"adriane/internal/workflow"
)

type ControlPlane struct {
	Store   *store.Store
	Bus     events.EventBus
	Stack   *runtime.Stack
	Agent   *agents.AgentService
	Auth    *auth.Authenticator
	Quota   *quota.Service
	Leader  *leader.Election
	Limiter *ratelimit.Limiter
	Logger  *slog.Logger

	remoteBus *events.NATSBus
	workers   *autoscale.Manager
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

	authenticator := auth.New(st.APIKeys, st.Orgs)
	if cfg.AuthEnabled {
		if err := authenticator.EnsureSeed(ctx, cfg.AdminAPIKey); err != nil {
			st.Close()
			bus.Close()
			return nil, err
		}
	}

	quotaSvc := quota.New(st, quota.Limits{
		MaxConcurrentAgents: cfg.OrgMaxConcurrentAgents,
		MaxDailyAgents:      cfg.OrgMaxDailyAgents,
		DailyCostCap:        cfg.OrgDailyCostCap,
	})

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
	agentService := agents.NewAgentService(st, bus, agents.NewTemplateRegistry(), plannerIf, compiler, engine, sched, quotaSvc, stack.Metrics, logger)

	cp := &ControlPlane{
		Store: st, Bus: bus, Stack: stack, Agent: agentService,
		Auth: authenticator, Quota: quotaSvc, Limiter: ratelimit.New(cfg.RateLimitPerMin),
		Logger: logger, remoteBus: remoteBus,
	}

	// Leadership: exactly one replica runs agent executions and the autoscaler.
	if cfg.LeaderEnabled {
		cp.Leader = leader.New(st.Pool(), logger)
		var workers *autoscale.Manager
		if cfg.WorkerAutoscale && cfg.WorkerMode == "remote" {
			workers = autoscale.NewManager(cfg.WorkerBinary, logger)
			cp.workers = workers
		}
		go cp.Leader.Run(ctx, func(ctx context.Context) {
			if workers != nil {
				go autoscale.NewAutoscalerPoll(workers, sched.Depth,
					cfg.WorkerMin, cfg.WorkerMax, cfg.ScaleUpThreshold, cfg.ScaleDownIdle,
					cfg.AutoscalerPoll, logger).Run(ctx)
			}
		})
	} else {
		logger.Warn("leader election disabled; assume this instance is leader")
	}

	return cp, nil
}

func (c *ControlPlane) IsLeader() bool {
	if c.Leader == nil {
		return true
	}
	return c.Leader.IsLeader()
}

func (c *ControlPlane) Close() {
	if c.Leader != nil {
		c.Leader.Close()
	}
	if c.workers != nil {
		c.workers.StopAll()
	}
	if c.remoteBus != nil {
		_ = c.remoteBus.Close()
	}
	_ = c.Bus.Close()
	c.Store.Close()
}
