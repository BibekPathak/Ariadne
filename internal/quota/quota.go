package quota

import (
	"context"
	"fmt"
	"time"

	"adriane/internal/store"
)

// Limits are per-organization quotas.
type Limits struct {
	MaxConcurrentAgents int
	MaxDailyAgents      int
	DailyCostCap        float64
	PricePer1k          float64 // blended token cost used for the cost cap
}

// Service enforces per-org quotas using the store.
type Service struct {
	store  *store.Store
	limits Limits
}

func New(s *store.Store, l Limits) *Service {
	if l.PricePer1k <= 0 {
		l.PricePer1k = 0.003 // coding tier default
	}
	return &Service{store: s, limits: l}
}

// CheckCreate verifies an org may create another agent. Returns an error with
// a user-facing message when a quota is exceeded.
func (q *Service) CheckCreate(ctx context.Context, orgID string) error {
	if q.limits.MaxConcurrentAgents > 0 {
		n, err := q.store.Agents.CountByOrgStatuses(ctx, orgID, "created", "running")
		if err != nil {
			return err
		}
		if n >= q.limits.MaxConcurrentAgents {
			return fmt.Errorf("quota exceeded: %d concurrent agents (limit %d)", n, q.limits.MaxConcurrentAgents)
		}
	}
	if q.limits.MaxDailyAgents > 0 {
		day := time.Now().UTC().Truncate(24 * time.Hour)
		n, err := q.store.Agents.CountByOrgCreatedSince(ctx, orgID, day)
		if err != nil {
			return err
		}
		if n >= q.limits.MaxDailyAgents {
			return fmt.Errorf("quota exceeded: %d agents today (limit %d)", n, q.limits.MaxDailyAgents)
		}
	}
	if q.limits.DailyCostCap > 0 {
		cost, err := q.DailyCost(ctx, orgID)
		if err != nil {
			return err
		}
		if cost >= q.limits.DailyCostCap {
			return fmt.Errorf("quota exceeded: daily cost $%.4f (cap $%.4f)", cost, q.limits.DailyCostCap)
		}
	}
	return nil
}

// Usage is the current consumption for an org.
type Usage struct {
	Agents          int     `json:"agents"`
	ActiveAgents    int     `json:"active_agents"`
	Tokens          int     `json:"tokens"`
	Cost            float64 `json:"cost"`
	QuotaRemaining  float64 `json:"quota_remaining"`
	ConcurrentLimit int     `json:"concurrent_limit"`
	DailyAgentLimit int     `json:"daily_agent_limit"`
	DailyCostCap    float64 `json:"daily_cost_cap"`
}

func (q *Service) Usage(ctx context.Context, orgID string) (*Usage, error) {
	agents, err := q.store.Agents.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	active := 0
	for _, a := range agents {
		if a.Status == "created" || a.Status == "running" {
			active++
		}
	}
	tokens, err := q.store.Events.TokensByOrgSince(ctx, orgID, time.Time{})
	if err != nil {
		return nil, err
	}
	cost := float64(tokens) / 1000 * q.limits.PricePer1k
	u := &Usage{
		Agents: len(agents), ActiveAgents: active, Tokens: tokens, Cost: cost,
		ConcurrentLimit: q.limits.MaxConcurrentAgents,
		DailyAgentLimit: q.limits.MaxDailyAgents,
		DailyCostCap:    q.limits.DailyCostCap,
	}
	if q.limits.DailyCostCap > 0 {
		u.QuotaRemaining = q.limits.DailyCostCap - cost
		if u.QuotaRemaining < 0 {
			u.QuotaRemaining = 0
		}
	}
	return u, nil
}

func (q *Service) DailyCost(ctx context.Context, orgID string) (float64, error) {
	tokens, err := q.store.Events.TokensByOrgSince(ctx, orgID, time.Now().UTC().Truncate(24*time.Hour))
	if err != nil {
		return 0, err
	}
	return float64(tokens) / 1000 * q.limits.PricePer1k, nil
}
