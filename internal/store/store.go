package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool      *pgxpool.Pool
	Agents    *AgentsRepo
	Tasks     *TasksRepo
	Events    *EventsRepo
	Artifacts *ArtifactsRepo
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
	return s, nil
}

func (s *Store) Close() { s.pool.Close() }
