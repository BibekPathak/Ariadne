package workflow

import (
	"fmt"

	"kubeai/internal/planner"
	"kubeai/internal/tasks"
)

// Compiler is the deterministic bridge between the AI Planner and the DAG
// engine. It validates template references, resolves named dependencies and
// assigns concrete node IDs. Being pure, it is trivially testable.
type Compiler struct {
	registry *tasks.Registry
}

func NewCompiler(registry *tasks.Registry) *Compiler {
	return &Compiler{registry: registry}
}

func (c *Compiler) Compile(agentID string, plan *planner.ExecutionPlan) (*DAG, error) {
	dag := &DAG{AgentID: agentID}

	// First pass: materialise nodes, validate templates.
	nameToNode := map[string]*Node{}
	for _, item := range plan.Tasks {
		tpl, ok := c.registry.Get(item.Template)
		if !ok {
			return nil, fmt.Errorf("compile: unknown template %q", item.Template)
		}
		node := &Node{
			ID:         fmt.Sprintf("%s_%s", agentID, item.Name),
			AgentID:    agentID,
			Name:       item.Name,
			Template:   item.Template,
			Inputs:     item.Inputs,
			Status:     StatusPending,
			MaxAttempt: tpl.Retries + 1,
		}
		if node.Inputs == nil {
			node.Inputs = map[string]any{}
		}
		dag.Nodes = append(dag.Nodes, node)
		nameToNode[item.Name] = node
	}

	// Second pass: resolve named dependencies into node IDs.
	for _, item := range plan.Tasks {
		node := nameToNode[item.Name]
		for _, depName := range item.DependsOn {
			dep, ok := nameToNode[depName]
			if !ok {
				return nil, fmt.Errorf("compile: task %q depends on unknown task %q", item.Name, depName)
			}
			if dep == node {
				return nil, fmt.Errorf("compile: task %q cannot depend on itself", item.Name)
			}
			node.DependsOn = append(node.DependsOn, dep.ID)
		}
	}
	return dag, nil
}
