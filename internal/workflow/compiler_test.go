package workflow

import (
	"testing"

	"adriane/internal/planner"
	"adriane/internal/tasks"
)

func TestCompilerLinearChain(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "a", Template: "analyze"},
		{Name: "b", Template: "implement", DependsOn: []string{"a"}},
		{Name: "c", Template: "test", DependsOn: []string{"b"}},
	}}
	dag, err := c.Compile("agent1", "r1", plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(dag.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(dag.Nodes))
	}
	ready := dag.Ready()
	if len(ready) != 1 || ready[0].Name != "a" {
		t.Fatalf("only 'a' should be ready, got %v", ready)
	}
	if ready[0].MaxAttempt != 2 { // test template: retries 1 -> max 2; analyze retries 1
		t.Fatalf("expected max_attempt 2, got %d", ready[0].MaxAttempt)
	}
	// Mark a done, then b ready.
	ready[0].Status = StatusDone
	ready = dag.Ready()
	if len(ready) != 1 || ready[0].Name != "b" {
		t.Fatalf("expected 'b' ready, got %v", names(ready))
	}
}

func TestCompilerParallelBranches(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "analyze", Template: "analyze"},
		{Name: "code", Template: "implement", DependsOn: []string{"analyze"}},
		{Name: "docs", Template: "review", DependsOn: []string{"analyze"}},
		{Name: "final", Template: "review", DependsOn: []string{"code", "docs"}},
	}}
	dag, err := c.Compile("agent1", "r1", plan)
	if err != nil {
		t.Fatal(err)
	}
	// After analyze done, both code and docs should be ready concurrently.
	analyze := dag.byName("analyze")
	analyze.Status = StatusDone
	ready := dag.Ready()
	if len(ready) != 2 {
		t.Fatalf("expected 2 parallel ready nodes, got %v", names(ready))
	}
	// Complete both, final becomes ready.
	for _, n := range ready {
		n.Status = StatusDone
	}
	ready = dag.Ready()
	if len(ready) != 1 || ready[0].Name != "final" {
		t.Fatalf("expected 'final' ready, got %v", names(ready))
	}
}

func TestCompilerUnknownTemplate(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "x", Template: "nope"},
	}}
	if _, err := c.Compile("agent1", "r1", plan); err == nil {
		t.Fatal("expected error for unknown template")
	}
}

func TestCompilerUnknownDependency(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "a", Template: "analyze"},
		{Name: "b", Template: "implement", DependsOn: []string{"ghost"}},
	}}
	if _, err := c.Compile("agent1", "r1", plan); err == nil {
		t.Fatal("expected error for unknown dependency")
	}
}

func TestCompilerSelfDependency(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "a", Template: "analyze", DependsOn: []string{"a"}},
	}}
	if _, err := c.Compile("agent1", "r1", plan); err == nil {
		t.Fatal("expected error for self dependency")
	}
}

func TestDAGBlocked(t *testing.T) {
	c := NewCompiler(tasks.NewRegistry())
	plan := &planner.ExecutionPlan{Tasks: []planner.PlanItem{
		{Name: "a", Template: "analyze"},
		{Name: "b", Template: "implement", DependsOn: []string{"a"}},
		{Name: "c", Template: "implement", DependsOn: []string{"b"}},
	}}
	dag, _ := c.Compile("agent1", "r1", plan)
	dag.byName("a").Status = StatusFailed
	dag.Blocked()
	if dag.byName("b").Status != StatusBlocked {
		t.Fatal("b should be blocked when a failed")
	}
	if dag.byName("c").Status != StatusBlocked {
		t.Fatal("c should be blocked transitively")
	}
	if !dag.AnyFailed() {
		t.Fatal("AnyFailed should be true")
	}
}

func names(nodes []*Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}
