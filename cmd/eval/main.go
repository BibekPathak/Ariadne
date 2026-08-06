package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"adriane/internal/config"
	"adriane/internal/controlplane"
	"adriane/internal/eval"
	"adriane/internal/router"
	"adriane/internal/store"
)

func main() {
	suitePath := flag.String("suite", "eval/suites/v1/coder.yaml", "path to the eval suite YAML")
	armFlag := flag.String("tier", "coding", "comma-separated router arms to evaluate (fast,coding,reasoning)")
	repeat := flag.Int("repeat", 1, "how many times to run each arm")
	outDir := flag.String("out", "evals", "artifact output directory")
	useBaseline := flag.Bool("baseline", true, "compare against the previous run of the same suite+arm")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx := context.Background()

	suite, err := eval.LoadSuite(*suitePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
	arms := split(*armFlag)
	if len(arms) == 0 {
		arms = []string{"coding"}
	}

	cfg := config.Load()
	offline := cfg.RouterPrimaryKey == ""
	cp, err := controlplane.Build(ctx, cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
	defer cp.Close()

	pricing := router.DefaultPricing()
	var rows []eval.LeaderboardRow
	regressionAny := false

	for _, tier := range arms {
		for i := 0; i < *repeat; i++ {
			// Baseline must be fetched before this run completes and becomes "latest".
			var baseline *store.EvalRun
			if *useBaseline {
				baseline, _ = cp.Store.EvalRuns.Latest(ctx, suite.Name, tier)
			}

			runner := eval.NewRunner(cp.Agent, cp.Store.EvalRuns, cp.Store.EvalResults, pricing, tier, "")
			run, err := runner.RunSuite(ctx, suite)
			if err != nil {
				fmt.Fprintln(os.Stderr, "eval:", tier, err)
				os.Exit(1)
			}
			results, _ := cp.Store.EvalResults.ListByRun(ctx, run.ID)

			dir, err := eval.WriteArtifacts(*outDir, run, results, offline)
			if err != nil {
				fmt.Fprintln(os.Stderr, "eval:", err)
				os.Exit(1)
			}

			curr := summaryOf(run)
			row := eval.LeaderboardRow{
				Arm: run.Arm, Suite: run.Suite, Tasks: curr.Tasks,
				SuccessPct: curr.SuccessPct, PassPct: curr.PassPct,
				AvgLatencyMs: curr.AvgLatencyMs, AvgCost: curr.AvgCost,
				ToolErrors: curr.ToolErrors, Tokens: curr.Tokens,
			}
			rows = append(rows, row)

			fmt.Printf("\n== arm %-9s %s (%s)\n", tier, run.ID, dir)
			if baseline != nil {
				reg := eval.CheckRegression(curr, summaryOf(baseline), suite.Thresholds)
				fmt.Println("   regression vs " + baseline.ID + ": " + reg.String())
				if reg.Breached {
					regressionAny = true
				}
			} else {
				fmt.Println("   (no baseline yet)")
			}
		}
	}

	fmt.Println("\n== leaderboard ==")
	fmt.Printf("  %-9s %-10s %6s %6s %10s %10s %10s %6s\n",
		"arm", "suite", "tasks", "success", "latency_ms", "cost", "tool_err", "tokens")
	for _, r := range rows {
		fmt.Printf("  %-9s %-10s %6d %5.1f%% %9.0fms %9.4f %10d %6d\n",
			r.Arm, r.Suite, r.Tasks, r.SuccessPct, r.AvgLatencyMs, r.AvgCost, r.ToolErrors, r.Tokens)
	}
	if offline {
		fmt.Println("\nnote: offline mode uses a shared mock provider; set ROUTER_*_MODEL to differentiate arms.")
	}
	if regressionAny {
		fmt.Println("\nREGRESSION DETECTED")
		os.Exit(1)
	}
}

func summaryOf(run *store.EvalRun) eval.Summary {
	return eval.Summary{
		Tasks: intNum(run.Summary["tasks"]), Passed: intNum(run.Summary["passed"]), Succeeded: intNum(run.Summary["succeeded"]),
		SuccessPct: floatNum(run.Summary["success_pct"]), PassPct: floatNum(run.Summary["pass_pct"]),
		AvgLatencyMs: floatNum(run.Summary["avg_latency_ms"]), AvgCost: floatNum(run.Summary["avg_cost"]),
		ToolErrors: intNum(run.Summary["tool_errors"]), Tokens: intNum(run.Summary["tokens"]),
	}
}

func intNum(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func floatNum(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func split(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
