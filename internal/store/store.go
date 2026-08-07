package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool        *pgxpool.Pool
	Agents      *AgentsRepo
	Tasks       *TasksRepo
	Events      *EventsRepo
	Artifacts   *ArtifactsRepo
	Memories    *MemoryRepo
	Checkpoints *CheckpointRepo
	EvalRuns    *EvalRunsRepo
	EvalResults *EvalResultsRepo
	Orgs        *OrgsRepo
	Users       *UsersRepo
	APIKeys     *APIKeysRepo
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &Store{pool: pool}
	s.Agents = &AgentsRepo{pool: pool}
	s.Tasks = &TasksRepo{pool: pool}
	s.Events = &EventsRepo{pool: pool}
	s.Artifacts = &ArtifactsRepo{pool: pool}
	s.Memories = &MemoryRepo{pool: pool}
	s.Checkpoints = &CheckpointRepo{pool: pool}
	s.EvalRuns = &EvalRunsRepo{pool: pool}
	s.EvalResults = &EvalResultsRepo{pool: pool}
	s.Orgs = &OrgsRepo{pool: pool}
	s.Users = &UsersRepo{pool: pool}
	s.APIKeys = &APIKeysRepo{pool: pool}
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the underlying connection pool (used for leader election).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
