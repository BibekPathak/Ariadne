package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"adriane/internal/events"
)

func TestLoadSuite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.yaml")
	os.WriteFile(path, []byte(`
name: test-suite
version: 2
thresholds:
  success_drop_percent: 5
tasks:
  - name: t1
    goal: "do a thing"
    repo: ./demo/repo
`), 0o644)

	s, err := LoadSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "test-suite" || s.Version != 2 {
		t.Fatalf("unexpected suite %+v", s)
	}
	if len(s.Tasks) != 1 || s.Tasks[0].Template != "coder" {
		t.Fatalf("template default missing: %+v", s.Tasks)
	}
	if s.Thresholds.SuccessDropPercent != 5 {
		t.Fatalf("thresholds not loaded: %+v", s.Thresholds)
	}
}

func TestLoadSuiteDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.yaml")
	os.WriteFile(path, []byte("name: x\ntasks:\n  - name: a\n    goal: g\n"), 0o644)
	s, _ := LoadSuite(path)
	if s.Version != 1 {
		t.Fatalf("default version should be 1, got %d", s.Version)
	}
	if s.Thresholds.SuccessDropPercent != 10 {
		t.Fatalf("default success threshold should be 10, got %v", s.Thresholds.SuccessDropPercent)
	}
}

func TestLoadSuiteInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("tasks: []\n"), 0o644)
	if _, err := LoadSuite(path); err == nil {
		t.Fatal("expected error for empty suite")
	}
}

func TestCompositeJudge(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg/mathx.go"), []byte("func Subtract(a, b int) int { return a - b }\n"), 0o644)

	j := CompositeJudge{Expected: Expected{
		MustSucceed: true,
		Files:       []string{"pkg/mathx.go"},
		Contains:    []string{"func Subtract"},
	}, WorkDir: dir}

	v, err := j.Score(context.Background(), Outcome{Success: true})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pass {
		t.Fatalf("expected pass, got %+v", v)
	}

	// Missing content fails.
	j.Expected.Contains = []string{"func Multiply"}
	v, _ = j.Score(context.Background(), Outcome{Success: true})
	if v.Pass {
		t.Fatal("expected failure when content is missing")
	}

	// Failed run with must_succeed fails.
	j.Expected.Contains = []string{"func Subtract"}
	v, _ = j.Score(context.Background(), Outcome{Success: false, Error: "boom"})
	if v.Pass {
		t.Fatal("expected failure when agent did not complete")
	}
}

func TestMetricsFromEvents(t *testing.T) {
	base := time.Now().UTC()
	evs := []events.Event{{
		Type: events.TaskStarted, TS: base,
	}, {
		Type: events.LLMCalled, TS: base.Add(100 * time.Millisecond),
		Payload: map[string]any{"total_tokens": float64(1000)},
	}, {
		Type: events.ToolFinished, TS: base.Add(200 * time.Millisecond),
		Payload: map[string]any{"error": true},
	}, {
		Type: events.ToolFinished, TS: base.Add(300 * time.Millisecond),
		Payload: map[string]any{"error": false},
	}, {
		Type: events.TaskFinished, TS: base.Add(500 * time.Millisecond),
	}}
	m := MetricsFromEvents(evs, 0.01)
	if m.LatencyMs != 500 {
		t.Fatalf("latency = %d, want 500", m.LatencyMs)
	}
	if m.Tokens != 1000 {
		t.Fatalf("tokens = %d, want 1000", m.Tokens)
	}
	if m.Cost != 0.01 {
		t.Fatalf("cost = %f, want 0.01", m.Cost)
	}
	if m.ToolErrors != 1 {
		t.Fatalf("tool errors = %d, want 1", m.ToolErrors)
	}
}

func TestCheckRegression(t *testing.T) {
	base := Summary{Tasks: 2, SuccessPct: 100, AvgLatencyMs: 1000, AvgCost: 0.1, ToolErrors: 0}

	// No breach.
	reg := CheckRegression(Summary{Tasks: 2, SuccessPct: 95, AvgLatencyMs: 1100, AvgCost: 0.11, ToolErrors: 0}, base, DefaultThresholds())
	if reg.Breached {
		t.Fatalf("unexpected regression: %v", reg.Details)
	}

	// Success drop breaches.
	reg = CheckRegression(Summary{Tasks: 2, SuccessPct: 50, AvgLatencyMs: 1000, AvgCost: 0.1, ToolErrors: 0}, base, DefaultThresholds())
	if !reg.Breached {
		t.Fatal("expected success regression")
	}

	// Cost increase breaches.
	reg = CheckRegression(Summary{Tasks: 2, SuccessPct: 100, AvgLatencyMs: 1000, AvgCost: 0.5, ToolErrors: 0}, base, DefaultThresholds())
	if !reg.Breached {
		t.Fatal("expected cost regression")
	}

	// No baseline -> no breach.
	reg = CheckRegression(Summary{Tasks: 2}, Summary{}, DefaultThresholds())
	if reg.Breached {
		t.Fatal("empty baseline should not breach")
	}
}
