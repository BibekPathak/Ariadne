// Package router selects the model and gateway for each LLM call. It is
// policy-based and provider-agnostic: callers describe what the task needs
// (task type, latency/cost budgets, tool calling, structured output) and the
// router maps that to a logical tier (fast/coding/reasoning/vision), then to a
// model name and gateway. Gateways are OpenAI-compatible endpoints; the same
// Router works with Requesty, OpenRouter, direct providers, or a local model.
package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"adriane/internal/llm"
	"adriane/internal/obs"
)

// Pricing maps a router tier to a blended cost per 1000 tokens. Placeholder
// values; tune in deployment.
type Pricing map[string]float64

func DefaultPricing() Pricing {
	return Pricing{"fast": 0.001, "coding": 0.003, "reasoning": 0.01}
}

func (p Pricing) PricePer1k(tier Tier) float64 {
	if v, ok := p[string(tier)]; ok {
		return v
	}
	return p["coding"]
}

func (p Pricing) Cost(tier Tier, tokens int) float64 {
	return float64(tokens) / 1000.0 * p.PricePer1k(tier)
}

type Tier string

const (
	TierFast      Tier = "fast"
	TierCoding    Tier = "coding"
	TierReasoning Tier = "reasoning"
	TierVision    Tier = "vision"
)

// RouteRequest describes what a call needs. It never mentions specific models.
type RouteRequest struct {
	TaskType                 string        // "planner" or a task template name
	TierHint                 Tier          // optional explicit tier
	LatencyBudget            time.Duration // if set and small, prefer fast
	CostBudget               float64       // if set and small, avoid reasoning
	RequiresToolCalling      bool
	RequiresStructuredOutput bool
}

// Policy constrains which tiers a caller is allowed to use.
type Policy struct {
	MinTier        Tier
	AllowFast      bool
	AllowReasoning bool
	AllowVision    bool
	DefaultTier    Tier
}

// WorkerPolicy is the default for agent-loop calls: any tier is fair game,
// and the caller's tier hint is respected.
func WorkerPolicy() Policy {
	return Policy{DefaultTier: TierCoding, AllowFast: true, AllowReasoning: true}
}

// PlannerPolicy keeps cheap models out of planning: at least coding, reasoning
// allowed for complex work, fast never.
func PlannerPolicy() Policy {
	return Policy{MinTier: TierCoding, AllowFast: false, AllowReasoning: true, DefaultTier: TierCoding}
}

// ResolvedRoute is the outcome of a routing decision.
type ResolvedRoute struct {
	Tier    Tier
	Model   string
	Gateway string
}

// gateway is one upstream endpoint.
type gateway struct {
	name     string
	provider llm.Provider
}

// Router implements llm.Provider: the worker, planner and memory manager all
// use it through the standard interface, while the worker additionally passes
// routing hints via GenerateRoute.
type Router struct {
	primary       *gateway
	fallback      *gateway
	tierModel     map[Tier]string
	defaultModel  string
	defaultPolicy Policy
	metrics       *obs.Metrics
	pricing       Pricing
}

type Config struct {
	FastModel      string
	CodingModel    string
	ReasoningModel string
	VisionModel    string
	PrimaryURL     string
	PrimaryKey     string
	FallbackURL    string
	FallbackKey    string
	EmbeddingModel string
	DefaultPolicy  Policy
	Metrics        *obs.Metrics
}

func New(cfg Config) *Router {
	r := &Router{
		tierModel: map[Tier]string{
			TierFast: cfg.FastModel, TierCoding: cfg.CodingModel,
			TierReasoning: cfg.ReasoningModel, TierVision: cfg.VisionModel,
		},
		defaultModel:  cfg.CodingModel,
		defaultPolicy: cfg.DefaultPolicy,
		metrics:       cfg.Metrics,
		pricing:       DefaultPricing(),
	}
	if cfg.PrimaryKey != "" {
		r.primary = &gateway{name: "primary", provider: llm.NewOpenAICompatible(llm.Config{
			Name: "primary", BaseURL: cfg.PrimaryURL, APIKey: cfg.PrimaryKey, EmbeddingModel: cfg.EmbeddingModel,
		})}
	} else {
		r.primary = &gateway{name: "demo", provider: llm.DemoProvider{}}
	}
	if cfg.FallbackKey != "" {
		r.fallback = &gateway{name: "fallback", provider: llm.NewOpenAICompatible(llm.Config{
			Name: "fallback", BaseURL: cfg.FallbackURL, APIKey: cfg.FallbackKey, EmbeddingModel: cfg.EmbeddingModel,
		})}
	}
	return r
}

// FromProvider wraps a single provider as a Router with no model catalog, for
// tests and single-provider deployments.
func FromProvider(p llm.Provider) *Router {
	return &Router{
		primary:       &gateway{name: p.Name(), provider: p},
		tierModel:     map[Tier]string{},
		defaultPolicy: WorkerPolicy(),
	}
}

func (r *Router) Name() string { return "router" }

// Generate implements llm.Provider using the default policy (planner policy by
// construction).
func (r *Router) Generate(ctx context.Context, req llm.Request) (*llm.Response, error) {
	resp, _, err := r.GenerateRoute(ctx, req, RouteRequest{}, r.defaultPolicy)
	return resp, err
}

// GenerateRoute resolves hints + policy to a backend, delegates, and fails over
// to the fallback gateway if the primary errors.
func (r *Router) GenerateRoute(ctx context.Context, req llm.Request, rr RouteRequest, pol Policy) (*llm.Response, ResolvedRoute, error) {
	start := time.Now()
	tier, model := r.resolve(rr, pol)
	req.Model = model
	route := ResolvedRoute{Tier: tier, Model: model}

	if r.primary == nil {
		return nil, route, fmt.Errorf("no gateway configured")
	}
	resp, err := r.primary.provider.Generate(ctx, req)
	if err == nil {
		route.Gateway = r.primary.name
		r.observe(resp, tier, start)
		return resp, route, nil
	}
	if r.fallback != nil {
		resp, err2 := r.fallback.provider.Generate(ctx, req)
		if err2 == nil {
			route.Gateway = r.fallback.name
			r.observe(resp, tier, start)
			return resp, route, nil
		}
		return nil, route, fmt.Errorf("primary %v; fallback %v", err, err2)
	}
	return nil, route, fmt.Errorf("primary %v", err)
}

func (r *Router) observe(resp *llm.Response, tier Tier, start time.Time) {
	if r.metrics == nil {
		return
	}
	r.metrics.RouterDecision.Observe(time.Since(start).Seconds())
	r.metrics.LLMCalls.Inc()
	if resp.Usage.TotalTokens > 0 {
		r.metrics.LLMTokens.WithLabelValues("prompt").Add(float64(resp.Usage.PromptTokens))
		r.metrics.LLMTokens.WithLabelValues("completion").Add(float64(resp.Usage.CompletionTokens))
		r.metrics.LLMTokens.WithLabelValues("total").Add(float64(resp.Usage.TotalTokens))
		r.metrics.Cost.Add(r.pricing.Cost(tier, resp.Usage.TotalTokens))
	}
}

// Embed implements llm.Provider, delegating to the primary (fallback on error).
func (r *Router) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if r.primary == nil {
		return nil, fmt.Errorf("no gateway configured")
	}
	v, err := r.primary.provider.Embed(ctx, texts)
	if err == nil {
		return v, nil
	}
	if r.fallback != nil {
		return r.fallback.provider.Embed(ctx, texts)
	}
	return nil, err
}

// resolve turns a route request + policy into a tier and model name.
func (r *Router) resolve(rr RouteRequest, pol Policy) (Tier, string) {
	tier := rr.TierHint
	if tier == "" {
		tier = pol.DefaultTier
	}
	if tier == "" {
		tier = TierCoding
	}
	// Structured output / planning never below coding.
	if rr.RequiresStructuredOutput || rr.TaskType == "planner" {
		if tierRank(tier) < tierRank(TierCoding) {
			tier = TierCoding
		}
	}
	// Tight latency budget prefers the fast tier when allowed.
	if rr.LatencyBudget > 0 && rr.LatencyBudget < 5*time.Second && pol.AllowFast {
		if tierRank(tier) > tierRank(TierFast) {
			tier = TierFast
		}
	}
	// Tight cost budget avoids the reasoning tier.
	if rr.CostBudget > 0 && rr.CostBudget < 0.01 && tier == TierReasoning {
		tier = TierCoding
	}
	// Clamp to policy.
	if tierRank(tier) < tierRank(pol.MinTier) {
		tier = pol.MinTier
	}
	if tier == TierFast && !pol.AllowFast {
		tier = TierCoding
	}
	if tier == TierReasoning && !pol.AllowReasoning {
		tier = TierCoding
	}
	if tier == TierVision && !pol.AllowVision {
		tier = TierCoding
	}

	model := r.tierModel[tier]
	if model == "" {
		model = r.tierModel[TierCoding]
	}
	if model == "" {
		model = r.defaultModel
	}
	return tier, model
}

func tierRank(t Tier) int {
	switch t {
	case TierFast:
		return 1
	case TierCoding:
		return 2
	case TierReasoning:
		return 3
	case TierVision:
		return 4
	default:
		return 0
	}
}

// IsComplex is a cheap heuristic used to escalate a call to the reasoning
// tier: long goals or goals mentioning architectural work.
func IsComplex(text string) bool {
	if len(text) > 400 {
		return true
	}
	lower := strings.ToLower(text)
	for _, k := range []string{"architect", "architecture", "refactor", "design", "complex", "multi-step", "optimize", "distributed"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
