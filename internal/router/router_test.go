package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"adriane/internal/llm"
)

func TestResolvePolicyClamp(t *testing.T) {
	r := &Router{tierModel: map[Tier]string{
		TierFast: "fast-model", TierCoding: "coding-model", TierReasoning: "reasoning-model",
	}, defaultModel: "coding-model"}

	cases := []struct {
		name string
		rr   RouteRequest
		pol  Policy
		want Tier
	}{
		{"fast allowed under worker policy", RouteRequest{TierHint: TierFast}, WorkerPolicy(), TierFast},
		{"fast clamped to coding under planner policy", RouteRequest{TierHint: TierFast}, PlannerPolicy(), TierCoding},
		{"reasoning allowed under planner policy", RouteRequest{TierHint: TierReasoning}, PlannerPolicy(), TierReasoning},
		{"structured output forces at least coding", RouteRequest{TierHint: TierFast, RequiresStructuredOutput: true}, WorkerPolicy(), TierCoding},
		{"planner task forces at least coding", RouteRequest{TierHint: TierFast, TaskType: "planner"}, WorkerPolicy(), TierCoding},
		{"tight latency prefers fast", RouteRequest{TierHint: TierCoding, LatencyBudget: 2 * time.Second}, WorkerPolicy(), TierFast},
		{"tight cost avoids reasoning", RouteRequest{TierHint: TierReasoning, CostBudget: 0.001}, WorkerPolicy(), TierCoding},
		{"min tier floors the result", RouteRequest{TierHint: TierFast}, Policy{MinTier: TierReasoning, AllowReasoning: true, DefaultTier: TierReasoning}, TierReasoning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, model := r.resolve(tc.rr, tc.pol)
			if tier != tc.want {
				t.Fatalf("tier = %s, want %s", tier, tc.want)
			}
			if model == "" {
				t.Fatal("expected a model to be resolved")
			}
		})
	}
}

func TestResolveModelBackfill(t *testing.T) {
	r := &Router{tierModel: map[Tier]string{TierCoding: "coding-model"}, defaultModel: "coding-model"}
	_, model := r.resolve(RouteRequest{TierHint: TierReasoning}, PlannerPolicy())
	if model != "coding-model" {
		t.Fatalf("unconfigured tier should backfill to coding model, got %q", model)
	}
}

func TestRouterFailoverToFallback(t *testing.T) {
	// Primary points at a dead endpoint; fallback is a healthy scripted provider.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	r := New(Config{PrimaryURL: deadURL, PrimaryKey: "k", DefaultPolicy: PlannerPolicy()})
	r.fallback = &gateway{name: "fallback", provider: llm.NewScriptedProvider("healthy",
		llm.Response{Content: "recovered"})}

	resp, route, err := r.GenerateRoute(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}, RouteRequest{}, WorkerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "recovered" {
		t.Fatalf("expected fallback response, got %q", resp.Content)
	}
	if route.Gateway != "fallback" {
		t.Fatalf("expected route through fallback gateway, got %q", route.Gateway)
	}
}

func TestRouterBothDown(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	r := New(Config{PrimaryURL: deadURL, PrimaryKey: "k", DefaultPolicy: PlannerPolicy()})
	r.fallback = &gateway{name: "fallback", provider: errProvider{}}

	if _, _, err := r.GenerateRoute(context.Background(), llm.Request{}, RouteRequest{}, WorkerPolicy()); err == nil {
		t.Fatal("expected an error when both gateways fail")
	}
}

// errProvider always fails, for failover tests.
type errProvider struct{}

func (errProvider) Name() string { return "err" }
func (errProvider) Generate(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return nil, context.DeadlineExceeded
}
func (errProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, context.DeadlineExceeded
}

func TestRouterAsProviderInterface(t *testing.T) {
	// The Router satisfies llm.Provider; the default policy is planner-like
	// (no fast tier).
	r := New(Config{PrimaryKey: "k", PrimaryURL: "http://unused.invalid", DefaultPolicy: PlannerPolicy()})
	r.primary = &gateway{name: "test", provider: llm.NewScriptedProvider("p", llm.Response{Content: "ok"})}
	var p llm.Provider = r
	resp, err := p.Generate(context.Background(), llm.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected %q", resp.Content)
	}
	if p.Name() != "router" {
		t.Fatalf("expected name router, got %q", p.Name())
	}
}
