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
	if len(plan.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].Template != "analyze" || plan.Tasks[1].Template != "implement" || plan.Tasks[2].Template != "test" {
		t.Fatalf("unexpected static plan %+v", plan.Tasks)
	}
}
