package planner

import "testing"

func TestParsePlanWithCodeFence(t *testing.T) {
	raw := "Here is the plan:\n```json\n{\"tasks\":[{\"name\":\"a\",\"template\":\"analyze\"},{\"name\":\"b\",\"template\":\"implement\",\"depends_on\":[\"a\"]}]}\n```"
	plan, err := parsePlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[1].DependsOn[0] != "a" {
		t.Fatalf("unexpected dependency %v", plan.Tasks[1].DependsOn)
	}
}

func TestParsePlanAssignsNames(t *testing.T) {
	plan, err := parsePlan(`{"tasks":[{"template":"test"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Tasks[0].Name != "task_1" {
		t.Fatalf("expected auto name task_1, got %q", plan.Tasks[0].Name)
	}
}

func TestParsePlanEmpty(t *testing.T) {
	if _, err := parsePlan(`{"tasks":[]}`); err == nil {
		t.Fatal("expected error for empty plan")
	}
}

func TestStaticPlanner(t *testing.T) {
	p := StaticPlanner{}
	plan, err := p.Plan(nil, PlanRequest{Goal: "build api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Template != "analyze" {
		t.Fatalf("expected analyze first, got %+v", plan.Tasks[0])
	}
	// implement and docs must both depend only on analyze (parallel branches).
	if len(plan.Tasks[1].DependsOn) != 1 || plan.Tasks[1].DependsOn[0] != "analyze" {
		t.Fatalf("implement should depend only on analyze, got %v", plan.Tasks[1].DependsOn)
	}
	if len(plan.Tasks[2].DependsOn) != 1 || plan.Tasks[2].DependsOn[0] != "analyze" {
		t.Fatalf("docs should depend only on analyze, got %v", plan.Tasks[2].DependsOn)
	}
	// test merges both branches.
	got := map[string]bool{}
	for _, d := range plan.Tasks[3].DependsOn {
		got[d] = true
	}
	if !got["implement"] || !got["docs"] {
		t.Fatalf("test should merge implement and docs, got %v", plan.Tasks[3].DependsOn)
	}
}
