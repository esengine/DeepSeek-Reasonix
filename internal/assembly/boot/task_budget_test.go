package boot

import (
	"testing"
	"time"

	"reasonix/internal/contract/config"
)

// Every axis ships off, so an unconfigured agent must reach the runtime with
// nothing set: this wiring exposes a bound, it does not choose one.
func TestUnconfiguredTaskBudgetIsEmpty(t *testing.T) {
	if got := taskBudgetFromConfig(&config.Config{}); got.Cost != 0 || got.Wall != 0 || got.Tokens != 0 {
		t.Fatalf("unconfigured task budget = %+v, want every axis unset", got)
	}
}

func TestConfiguredTaskBudgetReachesTheRuntime(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agent.TaskCostBudget = 0.5
	cfg.Agent.TaskTimeBudgetMinutes = 10
	cfg.Agent.TaskTokenBudget = 2_000_000

	got := taskBudgetFromConfig(cfg)
	if got.Cost != 0.5 {
		t.Errorf("Cost = %v, want 0.5", got.Cost)
	}
	if got.Wall != 10*time.Minute {
		t.Errorf("Wall = %v, want 10m", got.Wall)
	}
	if got.Tokens != 2_000_000 {
		t.Errorf("Tokens = %d, want 2000000 — the axis run_budget.go already enforces was not reaching it", got.Tokens)
	}
}
