// Package leader implements control-plane leadership via a PostgreSQL
// advisory lock. Exactly one replica holds the lock and runs the scheduler /
// autoscaler; standbys serve reads. If the leader dies, its database session
// drops and a standby acquires the lock.
package leader

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const lockKey = 424242

type Election struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu     sync.Mutex
	leader bool
	conn   *pgxpool.Conn

	stop chan struct{}
	once sync.Once
}

func New(pool *pgxpool.Pool, logger *slog.Logger) *Election {
	if logger == nil {
		logger = slog.Default()
	}
	return &Election{pool: pool, logger: logger, stop: make(chan struct{})}
}

func (e *Election) IsLeader() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.leader
}

// Run tries to become leader and, on success, invokes onLeader once. It
// monitors the lock and re-acquires it if the session drops. Blocks until ctx
// is cancelled.
func (e *Election) Run(ctx context.Context, onLeader func(context.Context)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stop:
			return
		default:
		}
		if e.tryAcquire(ctx) {
			e.becomeLeader(ctx, onLeader)
			e.monitor(ctx)
			// Always release the lock-held session when monitoring ends
			// (ctx cancel, stop, or a session error) so the pool connection
			// is returned.
			e.release()
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (e *Election) tryAcquire(ctx context.Context) bool {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		e.logger.Warn("leader: acquire session failed", "err", err)
		return false
	}
	var ok bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&ok); err != nil {
		conn.Release()
		e.logger.Warn("leader: advisory lock query failed", "err", err)
		return false
	}
	if !ok {
		conn.Release()
		return false
	}
	e.mu.Lock()
	e.conn = conn
	e.leader = true
	e.mu.Unlock()
	e.logger.Info("leader: acquired advisory lock")
	return true
}

func (e *Election) becomeLeader(ctx context.Context, onLeader func(context.Context)) {
	e.once.Do(func() {
		if onLeader != nil {
			go onLeader(ctx)
		}
	})
}

// monitor keeps the lock-held session alive; returns false if it drops.
func (e *Election) monitor(ctx context.Context) bool {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return true
		case <-e.stop:
			return true
		case <-tick.C:
			e.mu.Lock()
			conn := e.conn
			e.mu.Unlock()
			if conn == nil {
				return false
			}
			var one int
			if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
				e.logger.Warn("leader: lock session error; stepping down", "err", err)
				return false
			}
		}
	}
}

func (e *Election) release() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.conn != nil {
		// Releasing the pool connection closes the session, dropping the lock.
		e.conn.Release()
		e.conn = nil
	}
	if e.leader {
		e.logger.Warn("leader: leadership lost")
	}
	e.leader = false
}

func (e *Election) Close() {
	e.release()
	select {
	case <-e.stop:
	default:
		close(e.stop)
	}
}
