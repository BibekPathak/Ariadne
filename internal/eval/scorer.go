package eval

import (
	"time"

	"adriane/internal/events"
)

// Pricing maps a router tier to a blended cost per 1000 tokens. Placeholder
// values; tune in deployment.
type Pricing map[string]float64

func DefaultPricing() Pricing {
	return Pricing{"fast": 0.001, "coding": 0.003, "reasoning": 0.01}
}

func (p Pricing) PricePer1k(tier string) float64 {
	if v, ok := p[tier]; ok {
		return v
	}
	return p["coding"]
}

// Metrics aggregates an agent run's cost/latency/reliability from its events.
type Metrics struct {
	LatencyMs  int64
	Cost       float64
	Tokens     int
	ToolErrors int
}

// MetricsFromEvents computes metrics from the persisted event log. pricePer1k
// is the blended cost of the run's model tier.
func MetricsFromEvents(evs []events.Event, pricePer1k float64) Metrics {
	var m Metrics
	var start, end time.Time
	for _, e := range evs {
		switch e.Type {
		case events.TaskStarted:
			if start.IsZero() || e.TS.Before(start) {
				start = e.TS
			}
		case events.TaskFinished, events.TaskFailed:
			if end.IsZero() || e.TS.After(end) {
				end = e.TS
			}
		case events.LLMCalled:
			if tok, ok := e.Payload["total_tokens"].(float64); ok {
				m.Tokens += int(tok)
			}
		case events.ToolFinished:
			if err, ok := e.Payload["error"].(bool); ok && err {
				m.ToolErrors++
			}
		}
	}
	if !start.IsZero() && !end.IsZero() {
		m.LatencyMs = end.Sub(start).Milliseconds()
	}
	m.Cost = float64(m.Tokens) / 1000.0 * pricePer1k
	return m
}
