package worker

import (
	"adriane/internal/router"
	"adriane/internal/tasks"
)

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
