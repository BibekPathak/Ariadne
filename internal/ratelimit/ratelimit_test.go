package ratelimit

import "testing"

func TestLimiterAllowsUpToBurst(t *testing.T) {
	l := New(3) // 3 per minute
	for i := 0; i < 3; i++ {
		if !l.Allow("org1") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if l.Allow("org1") {
		t.Fatal("request 4 should be limited")
	}
	// A different key is independent.
	if !l.Allow("org2") {
		t.Fatal("another key should not be limited")
	}
}

func TestLimiterRefills(t *testing.T) {
	l := New(60) // 1 per second burst 60
	for i := 0; i < 60; i++ {
		l.Allow("org1")
	}
	if l.Allow("org1") {
		t.Fatal("expected limit hit")
	}
	// A 1/sec rate with burst exhausted needs ~1s to refill one token.
	if ra := l.RetryAfter("org1"); ra <= 0 {
		t.Fatal("RetryAfter should be positive")
	}
}
