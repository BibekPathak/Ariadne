package worker

import (
	"adriane/internal/router"
	"adriane/internal/tasks"
	"adriane/internal/workflow"
)

// routeForTask derives routing hints from the task template, goal, and any
// per-agent tier override carried on the node inputs.
func routeForTask(node *workflow.Node, tpl tasks.Template, goal string) router.RouteRequest {
	rr := routeFor(tpl, goal)
	if tier, ok := node.Inputs["tier"].(string); ok && tier != "" {
		rr.TierHint = router.Tier(tier)
	}
	return rr
}

// routeFor derives routing hints from the task template and goal. Template
// governs the default tier; goal complexity can escalate to reasoning.
func routeFor(tpl tasks.Template, goal string) router.RouteRequest {
	rr := router.RouteRequest{TaskType: tpl.Name, RequiresToolCalling: true}
	switch tpl.Name {
	case "test", "analyze":
		rr.TierHint = router.TierFast
	case "implement", "docs", "review", "git_clone":
		rr.TierHint = router.TierCoding
	}
	if router.IsComplex(goal) {
		rr.TierHint = router.TierReasoning
	}
	return rr
}
