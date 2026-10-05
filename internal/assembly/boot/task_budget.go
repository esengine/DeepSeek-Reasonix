package boot

import (
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/agent"
)

// taskBudgetFromConfig maps the configured spend gates onto the agent budget.
// Zero and negative values disable the time axis; only a positive value is an
// explicit wall-clock budget. normalizeTaskBudget reads a negative token or
// cost budget as unset, so this passes them through unchanged.
func taskBudgetFromConfig(cfg *config.Config) agent.TaskBudget {
	b := agent.TaskBudget{Cost: cfg.Agent.TaskCostBudget, Tokens: cfg.Agent.TaskTokenBudget}
	switch minutes := cfg.Agent.TaskTimeBudgetMinutes; {
	case minutes < 0:
		b.Wall = -1
	case minutes > 0:
		b.Wall = time.Duration(minutes * float64(time.Minute))
	}
	return b
}
