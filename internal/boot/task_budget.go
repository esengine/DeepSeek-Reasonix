package boot

import (
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// taskBudgetFromConfig maps the configured spend gate onto the agent budget.
// Zero and negative values disable the time axis; only a positive value is an
// explicit wall-clock budget.
func taskBudgetFromConfig(cfg *config.Config) agent.TaskBudget {
	b := agent.TaskBudget{Cost: cfg.Agent.TaskCostBudget}
	switch minutes := cfg.Agent.TaskTimeBudgetMinutes; {
	case minutes < 0:
		b.Wall = -1
	case minutes > 0:
		b.Wall = time.Duration(minutes * float64(time.Minute))
	}
	return b
}

// progressBudgetRoundsFromConfig maps the configured progress-reassessment
// checkpoint onto the agent option. A disabled budget is passed through as the
// off sentinel so the agent never falls back to the built-in round count; an
// enabled one is normalized once here, at the single place the executor is
// built.
func progressBudgetRoundsFromConfig(cfg *config.Config) int {
	if !cfg.ProgressBudgetEnabled() {
		return agent.ProgressBudgetRoundsOff
	}
	return agent.NormalizeProgressBudgetRounds(cfg.ProgressBudgetRoundsValue())
}
