package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"adriane/internal/llm"
	"adriane/internal/router"
	"adriane/internal/tasks"
)

// PlanItem is one unit of work in an execution plan. Dependencies are
// expressed by name and resolved by the workflow compiler.
type PlanItem struct {
	Name      string         `json:"name"`
	Template  string         `json:"template"`
	Inputs    map[string]any `json:"inputs,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
}

type ExecutionPlan struct {
	Tasks []PlanItem `json:"tasks"`
}

// Planner is the AI front-end: it converts a goal into an ExecutionPlan.
// The plan is only intent; the deterministic Workflow Compiler turns it into
// a concrete DAG.
type Planner interface {
	Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error)
}

type PlanRequest struct {
	AgentID            string
	Goal               string
	RepoURL            string
	PlannerPrompt      string
	AvailableTemplates []string
}

// LLMPlanner asks the model to produce an execution plan expressed in task
// templates. The planner routes through the model router under the planner
// policy (at least coding tier, reasoning for complex goals).
type LLMPlanner struct {
	router   *router.Router
	registry *tasks.Registry
	policy   router.Policy
}

func NewLLMPlanner(rtr *router.Router, registry *tasks.Registry) *LLMPlanner {
	return &LLMPlanner{router: rtr, registry: registry, policy: router.PlannerPolicy()}
}

const planSchema = `You are an agent planner. Given a goal, break it into a DAG of tasks. Reply with JSON only:
{"tasks":[{"name":"<unique name>","template":"<one of the templates>","inputs":{},"depends_on":["<names of tasks this depends on>"]}]}
Keep dependencies minimal. Independent tasks may have empty depends_on.`

func (p *LLMPlanner) Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error) {
	user := fmt.Sprintf("GOAL: %s\nREPO: %s\nAVAILABLE TEMPLATES: %s",
		req.Goal, req.RepoURL, strings.Join(req.AvailableTemplates, ", "))
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: planSchema},
		{Role: llm.RoleUser, Content: user},
	}
	rr := router.RouteRequest{TaskType: "planner", RequiresStructuredOutput: true}
	if router.IsComplex(req.Goal) {
		rr.TierHint = router.TierReasoning
	}
	resp, _, err := p.router.GenerateRoute(ctx, llm.Request{Messages: msgs}, rr, p.policy)
	if err != nil {
		return nil, fmt.Errorf("planner llm call: %w", err)
	}
	plan, err := parsePlan(resp.Content)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func parsePlan(raw string) (*ExecutionPlan, error) {
	raw = extractJSON(raw)
	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return nil, fmt.Errorf("parse plan json: %w", err)
	}
	if len(plan.Tasks) == 0 {
		return nil, fmt.Errorf("planner produced no tasks")
	}
	for i := range plan.Tasks {
		if plan.Tasks[i].Name == "" {
			plan.Tasks[i].Name = fmt.Sprintf("task_%d", i+1)
		}
		if plan.Tasks[i].Template == "" {
			return nil, fmt.Errorf("task %q missing template", plan.Tasks[i].Name)
		}
		if plan.Tasks[i].Inputs == nil {
			plan.Tasks[i].Inputs = map[string]any{}
		}
	}
	return &plan, nil
}

func extractJSON(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return s
	}
	return s[start : end+1]
}

// StaticPlanner is the deterministic fallback used when no LLM key is
// configured. It emits the coder pipeline as a real branching DAG so the
// offline demo exercises parallel execution: analyze -> {implement, docs} ->
// test. It keeps the whole system runnable offline.
type StaticPlanner struct{}

func (StaticPlanner) Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error) {
	repo := map[string]any{}
	if req.RepoURL != "" {
		repo["repo_url"] = req.RepoURL
	}
	return &ExecutionPlan{Tasks: []PlanItem{
		{Name: "analyze", Template: "analyze", Inputs: repo},
		{Name: "implement", Template: "implement", Inputs: repo, DependsOn: []string{"analyze"}},
		{Name: "docs", Template: "docs", Inputs: repo, DependsOn: []string{"analyze"}},
		{Name: "test", Template: "test", Inputs: repo, DependsOn: []string{"implement", "docs"}},
	}}, nil
}
