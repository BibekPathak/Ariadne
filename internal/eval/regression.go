package eval

import (
	"fmt"
	"strings"
)

// Regression compares the current run against a baseline using per-metric
// thresholds. Any breach flags a regression.
type Regression struct {
	Breached bool
	Details  []string
}

func CheckRegression(curr, baseline Summary, th Thresholds) Regression {
	if baseline.Tasks == 0 {
		return Regression{Breached: false, Details: []string{"no baseline"}}
	}
	var reg Regression
	check := func(format string, args ...any) {
		reg.Details = append(reg.Details, fmt.Sprintf(format, args...))
	}

	if th.SuccessDropPercent > 0 && curr.SuccessPct < baseline.SuccessPct-th.SuccessDropPercent {
		reg.Breached = true
		check("success dropped %.1f%% -> %.1f%% (threshold %.1f%%)",
			baseline.SuccessPct, curr.SuccessPct, th.SuccessDropPercent)
	}
	if th.LatencyIncreasePercent > 0 && baseline.AvgLatencyMs > 0 {
		inc := 100 * (curr.AvgLatencyMs - baseline.AvgLatencyMs) / baseline.AvgLatencyMs
		if inc > th.LatencyIncreasePercent {
			reg.Breached = true
			check("latency increased %.0f%% (%.0fms -> %.0fms, threshold %.0f%%)",
				inc, baseline.AvgLatencyMs, curr.AvgLatencyMs, th.LatencyIncreasePercent)
		}
	}
	if th.CostIncreasePercent > 0 && baseline.AvgCost > 0 {
		inc := 100 * (curr.AvgCost - baseline.AvgCost) / baseline.AvgCost
		if inc > th.CostIncreasePercent {
			reg.Breached = true
			check("cost increased %.0f%% ($%.4f -> $%.4f, threshold %.0f%%)",
				inc, baseline.AvgCost, curr.AvgCost, th.CostIncreasePercent)
		}
	}
	if th.ToolErrorsIncreasePercent > 0 && baseline.ToolErrors > 0 {
		inc := 100 * float64(curr.ToolErrors-baseline.ToolErrors) / float64(baseline.ToolErrors)
		if inc > th.ToolErrorsIncreasePercent {
			reg.Breached = true
			check("tool errors increased %.0f%% (%d -> %d, threshold %.0f%%)",
				inc, baseline.ToolErrors, curr.ToolErrors, th.ToolErrorsIncreasePercent)
		}
	}
	if len(reg.Details) == 0 {
		reg.Details = []string{"all metrics within thresholds"}
	}
	return reg
}

func (r Regression) String() string {
	return strings.Join(r.Details, "\n")
}
