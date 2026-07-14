package agent

import (
	"sync"
)

// StrategyDecision represents the result of a context window strategy evaluation.
// OPT-63: Context window management strategy selector.
type StrategyDecision struct {
	Action        string
	NewWindowSize int
	Reason        string
}

// ContextWindowStrategy decides context window management strategy based on
// conversation state such as token utilization and turn count.
type ContextWindowStrategy struct {
	mu                     sync.RWMutex
	strategy               string
	totalEvaluations       int
	strategyChanges        int
	lastWindowUtilization  float64
}

// WindowStrategyStats holds statistics about context window strategy evaluations.
type WindowStrategyStats struct {
	Strategy               string
	TotalEvaluations       int
	StrategyChanges        int
	LastWindowUtilization  float64
}

// NewContextWindowStrategy creates a new ContextWindowStrategy with the default
// strategy "grow".
func NewContextWindowStrategy() *ContextWindowStrategy {
	return &ContextWindowStrategy{
		strategy: "grow",
	}
}

// Evaluate examines the current conversation state and returns a strategy
// decision for context window management.
func (c *ContextWindowStrategy) Evaluate(usedTokens int, windowSize int, turnCount int) StrategyDecision {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalEvaluations++

	if windowSize <= 0 {
		c.lastWindowUtilization = 0
		return StrategyDecision{
			Action:        "grow",
			NewWindowSize: windowSize,
			Reason:        "invalid window size, maintaining current strategy",
		}
	}

	utilization := float64(usedTokens) / float64(windowSize)
	c.lastWindowUtilization = utilization

	var newStrategy string
	var decision StrategyDecision

	if utilization < 0.3 && turnCount > 3 {
		newStrategy = "shrink"
		decision = StrategyDecision{
			Action:        "shrink",
			NewWindowSize: windowSize * 2 / 3,
			Reason:        "low utilization with sufficient turns, shrinking window allocation",
		}
	} else if utilization > 0.8 {
		newStrategy = "compact"
		decision = StrategyDecision{
			Action:        "compact",
			NewWindowSize: windowSize,
			Reason:        "high utilization, triggering compaction",
		}
	} else {
		newStrategy = "grow"
		decision = StrategyDecision{
			Action:        "grow",
			NewWindowSize: windowSize,
			Reason:        "normal utilization, maintaining growth strategy",
		}
	}

	if newStrategy != c.strategy {
		c.strategyChanges++
		c.strategy = newStrategy
	}

	return decision
}

// GetStrategy returns the current active strategy.
func (c *ContextWindowStrategy) GetStrategy() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.strategy
}

// GetStats returns current statistics about strategy evaluations.
func (c *ContextWindowStrategy) GetStats() WindowStrategyStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return WindowStrategyStats{
		Strategy:              c.strategy,
		TotalEvaluations:      c.totalEvaluations,
		StrategyChanges:       c.strategyChanges,
		LastWindowUtilization: c.lastWindowUtilization,
	}
}

// Reset clears all evaluation statistics and resets the strategy to "grow".
func (c *ContextWindowStrategy) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.strategy = "grow"
	c.totalEvaluations = 0
	c.strategyChanges = 0
	c.lastWindowUtilization = 0
}
