package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a per-key token bucket.
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens refilled per second
	burst   int
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func New(perMinute int) *Limiter {
	if perMinute <= 0 {
		perMinute = 10
	}
	return &Limiter{
		rate:    float64(perMinute) / 60.0,
		burst:   perMinute,
		buckets: map[string]*bucket{},
	}
}

// Allow reports whether a request for key may proceed now.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(l.burst), last: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(float64(l.burst), b.tokens+elapsed*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RetryAfter estimates seconds until the bucket refills a token.
func (l *Limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, ok := l.buckets[key]; ok {
		need := 1 - b.tokens
		if need <= 0 {
			return time.Second
		}
		return time.Duration(need/l.rate*float64(time.Second)) + time.Second
	}
	return time.Second
}
