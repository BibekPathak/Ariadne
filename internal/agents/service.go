package agents

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"adriane/internal/auth"
	"adriane/internal/events"
	"adriane/internal/obs"
	"adriane/internal/planner"
	"adriane/internal/quota"
	"adriane/internal/scheduler"
	"adriane/internal/store"
	"adriane/internal/tasks"
	"adriane/internal/workflow"
)

const (
	StatusCreated   = "created"
	StatusRunning   = "running"
	StatusPaused    = "paused"
	StatusCancelled = "cancelled"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

type CreateRequest struct {
	Template string `json:"template"`
	Goal     string `json:"goal"`
	RepoURL  string `json:"repo_url"`
	RepoPath string `json:"repo_path"`
	// Tier pins every task of this agent to a router tier (fast/coding/
	// reasoning); empty lets the task characteristics decide.
	Tier string `json:"tier,omitempty"`
}

// AgentService is the control-plane orchestrator: it owns the agent lifecycle
// and drives planner -> compiler -> engine for each agent run.
type AgentService struct {
	store     *store.Store
	bus       events.EventBus
	templates *TemplateRegistry
	planner   planner.Planner
	compiler  *workflow.Compiler
	engine    *workflow.Engine
	scheduler *scheduler.Scheduler
	quota     *quota.Service
	metrics   *obs.Metrics
	logger    *slog.Logger
}

func NewAgentService(s *store.Store, bus events.EventBus, templates *TemplateRegistry,
	p planner.Planner, compiler *workflow.Compiler, engine *workflow.Engine,
	sched *scheduler.Scheduler, quotaSvc *quota.Service, metrics *obs.Metrics, logger *slog.Logger) *AgentService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentService{store: s, bus: bus, templates: templates, planner: p,
		compiler: compiler, engine: engine, scheduler: sched, quota: quotaSvc, metrics: metrics, logger: logger}
}

func (a *AgentService) Create(ctx context.Context, req CreateRequest) (*store.Agent, error) {
	principal := auth.PrincipalFromContext(ctx)
	if !principal.CanManage() {
		return nil, fmt.Errorf("forbidden: role %s cannot create agents", principal.Role)
	}
	tpl, ok := a.templates.Get(req.Template)
	if !ok {
		return nil, fmt.Errorf("unknown agent template %q (available: %v)", req.Template, a.templates.Names())
	}
	if req.Goal == "" {
		return nil, fmt.Errorf("goal is required")
	}
	if req.RepoURL == "" && req.RepoPath == "" {
		return nil, fmt.Errorf("repo_url or repo_path is required")
	}
	if a.quota != nil {
		if err := a.quota.CheckCreate(ctx, principal.OrgID); err != nil {
			return nil, err
		}
	}

	agent := &store.Agent{
		ID:       newID(),
		OrgID:    principal.OrgID,
		Template: req.Template,
		Goal:     req.Goal,
		RepoURL:  req.RepoURL,
		RepoPath: req.RepoPath,
		Status:   StatusCreated,
		Meta: map[string]any{
			"description": tpl.Description,
			"plan":        tpl.DefaultPlan,
		},
	}
	if req.Tier != "" {
		agent.Meta["tier"] = req.Tier
	}
	if err := a.store.Agents.Create(ctx, agent); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	_ = a.bus.Publish(ctx, events.New(agent.ID, "", events.AgentCreated, map[string]any{
		"template": agent.Template, "goal": agent.Goal,
	}))
	if a.metrics != nil {
		a.metrics.AgentsCreated.Inc()
	}
	return agent, nil
}

// Run drives an agent to completion: plan -> compile -> execute.
func (a *AgentService) Run(ctx context.Context, agentID string) error {
	agent, err := a.store.Agents.Get(ctx, agentID)
	if err != nil {
		return err
	}
	if err := a.store.Agents.UpdateStatus(ctx, agent.ID, StatusRunning, ""); err != nil {
		return err
	}

	tpl, _ := a.templates.Get(agent.Template)
	registry := tasks.NewRegistry()

	planStart := time.Now()
	plan, err := a.planner.Plan(ctx, planner.PlanRequest{
		AgentID:            agent.ID,
		Goal:               agent.Goal,
		RepoURL:            agent.RepoURL,
		PlannerPrompt:      tpl.PlannerPrompt,
		AvailableTemplates: registry.Names(),
	})
	if a.metrics != nil {
		a.metrics.PlannerTime.Observe(time.Since(planStart).Seconds())
	}
	if err != nil {
		a.fail(ctx, agent, err)
		return err
	}
	_ = a.bus.Publish(ctx, events.New(agent.ID, "", events.PlanCreated, map[string]any{
		"tasks": plan.Tasks,
	}))

	// The worker is stateless: it learns the repository location from each
	// task's inputs, so propagate it onto every plan item.
	for i := range plan.Tasks {
		plan.Tasks[i].Inputs["repo_url"] = agent.RepoURL
		plan.Tasks[i].Inputs["repo_path"] = agent.RepoPath
		plan.Tasks[i].Inputs["goal"] = agent.Goal
		if tier, ok := agent.Meta["tier"].(string); ok && tier != "" {
			plan.Tasks[i].Inputs["tier"] = tier
		}
	}

	dag, err := a.compiler.Compile(agent.ID, newRunID(), plan)
	if err != nil {
		a.fail(ctx, agent, err)
		return err
	}

	if err := a.engine.Run(ctx, dag, a.scheduler); err != nil {
		a.fail(ctx, agent, err)
		return err
	}

	if err := a.store.Agents.UpdateStatus(ctx, agent.ID, StatusCompleted, ""); err != nil {
		return err
	}
	if a.metrics != nil {
		a.metrics.AgentsCompleted.Inc()
	}
	_ = a.bus.Publish(ctx, events.New(agent.ID, "", events.AgentCompleted, map[string]any{}))
	return nil
}

func (a *AgentService) fail(ctx context.Context, agent *store.Agent, cause error) {
	if a.metrics != nil {
		a.metrics.AgentsFailed.Inc()
	}
	_ = a.store.Agents.UpdateStatus(ctx, agent.ID, StatusFailed, cause.Error())
	_ = a.bus.Publish(ctx, events.New(agent.ID, "", events.AgentFailed, map[string]any{"error": cause.Error()}))
}

func (a *AgentService) Get(ctx context.Context, id string) (*store.Agent, error) {
	return a.store.Agents.Get(ctx, id)
}

func (a *AgentService) List(ctx context.Context) ([]*store.Agent, error) {
	principal := auth.PrincipalFromContext(ctx)
	if principal.Role == auth.RoleAdmin {
		return a.store.Agents.List(ctx)
	}
	return a.store.Agents.ListByOrg(ctx, principal.OrgID)
}

func (a *AgentService) Usage(ctx context.Context) (*quota.Usage, error) {
	if a.quota == nil {
		return &quota.Usage{}, nil
	}
	return a.quota.Usage(ctx, auth.PrincipalFromContext(ctx).OrgID)
}

func (a *AgentService) OrgID(ctx context.Context) string {
	return auth.PrincipalFromContext(ctx).OrgID
}

// ListKeys returns the API keys for the caller's org.
func (a *AgentService) ListKeys(ctx context.Context, p *auth.Principal) ([]*store.APIKey, error) {
	return a.store.APIKeys.ListByOrg(ctx, p.OrgID)
}

func (a *AgentService) Graph(ctx context.Context, id string) ([]*store.Task, error) {
	return a.store.Tasks.ListByAgent(ctx, id)
}

func (a *AgentService) Events(ctx context.Context, id string) ([]events.Event, error) {
	return a.store.Events.ListByAgent(ctx, id)
}

func (a *AgentService) Artifacts(ctx context.Context, id string) ([]*store.Artifact, error) {
	return a.store.Artifacts.ListByAgent(ctx, id)
}

// ArtifactContent returns the file content of a task artifact, scoped to the
// agent so arbitrary host paths can't be read.
func (a *AgentService) ArtifactContent(ctx context.Context, agentID, artifactID string) ([]byte, error) {
	art, err := a.store.Artifacts.Get(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	if art.AgentID != agentID {
		return nil, fmt.Errorf("artifact does not belong to agent %s", agentID)
	}
	return os.ReadFile(art.Path)
}

func (a *AgentService) Templates() []string {
	return a.templates.Names()
}

func (a *AgentService) StreamAfter(ctx context.Context, id string, seq int64) ([]events.Event, error) {
	return a.store.Events.StreamAfterSeq(ctx, id, seq)
}
