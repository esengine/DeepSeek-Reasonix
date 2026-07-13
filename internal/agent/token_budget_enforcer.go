package agent

import (
	"sync"
)

// EnforcementAction represents the action to take when token budget is evaluated.
// OPT-62: Token budget enforcement with automatic degradation.
type EnforcementAction string

const (
	// EnforcementAllow indicates usage is within safe limits.
	EnforcementAllow EnforcementAction = "allow"
	// EnforcementWarn indicates usage is approaching the hard limit.
	EnforcementWarn EnforcementAction = "warn"
	// EnforcementDegrade indicates usage has exceeded the hard limit and
	// degradation measures (compaction, tool dropping, etc.) should be triggered.
	EnforcementDegrade EnforcementAction = "degrade"
)

// TokenBudgetEnforcer enforces hard token budgets per turn with automatic
// degradation when limits are exceeded.
type TokenBudgetEnforcer struct {
	mu               sync.RWMutex
	hardLimit        int
	softLimit        int
	currentUsage     int
	enforcementCount int
	degradedCount    int
	totalSaved       int
}

// BudgetEnforcerStats holds statistics about budget enforcement activity.
type BudgetEnforcerStats struct {
	HardLimit        int
	SoftLimit        int
	EnforcementCount int
	DegradedCount    int
	TotalSaved       int
}

// NewTokenBudgetEnforcer creates a new TokenBudgetEnforcer with the given hard
// limit. The soft limit is set to 80% of the hard limit.
func NewTokenBudgetEnforcer(hardLimit int) *TokenBudgetEnforcer {
	return &TokenBudgetEnforcer{
		hardLimit: hardLimit,
		softLimit: hardLimit * 80 / 100,
	}
}

// Enforce evaluates the current token usage against the configured limits and
// returns the appropriate enforcement action.
func (e *TokenBudgetEnforcer) Enforce(usage int) EnforcementAction {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentUsage = usage
	e.enforcementCount++

	if usage >= e.hardLimit {
		e.degradedCount++
		saved := usage - e.softLimit
		if saved > 0 {
			e.totalSaved += saved
		}
		return EnforcementDegrade
	}

	if usage >= e.softLimit {
		return EnforcementWarn
	}

	return EnforcementAllow
}

// SetLimits updates the hard and soft limits for the budget enforcer.
func (e *TokenBudgetEnforcer) SetLimits(hard, soft int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.hardLimit = hard
	e.softLimit = soft
}

// GetStats returns current statistics about budget enforcement.
func (e *TokenBudgetEnforcer) GetStats() BudgetEnforcerStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return BudgetEnforcerStats{
		HardLimit:        e.hardLimit,
		SoftLimit:        e.softLimit,
		EnforcementCount: e.enforcementCount,
		DegradedCount:    e.degradedCount,
		TotalSaved:       e.totalSaved,
	}
}

// Reset clears all enforcement statistics while preserving the configured limits.
func (e *TokenBudgetEnforcer) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentUsage = 0
	e.enforcementCount = 0
	e.degradedCount = 0
	e.totalSaved = 0
}
