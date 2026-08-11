package boot

import (
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// taskBudgetFromConfig maps the configured spend gate onto the agent budget.
// A negative value disables that axis; zero leaves the default in place.
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
