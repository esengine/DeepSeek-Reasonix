package daemon

import (
	"fmt"
	"time"

	"reasonix/internal/agent"
)

func reserveAutoWakeupBudget(runtime *agent.RuntimeMeta, source string, now time.Time) (bool, string) {
	if runtime == nil {
		return false, "runtime missing"
	}
	now = now.UTC()
	budget := &runtime.Budget
	resetBudgetWindowIfNeeded(budget, now)
	if budget.DailyWakeupLimit > 0 && budget.DailyWakeups >= budget.DailyWakeupLimit {
		reason := fmt.Sprintf("daily automatic wakeup budget exhausted for %s (%d/%d)", source, budget.DailyWakeups, budget.DailyWakeupLimit)
		budget.LastBlockedAt = now
		budget.LastBlockedReason = reason
		return false, reason
	}
	budget.DailyWakeups++
	return true, ""
}

func checkModelBudget(runtime *agent.RuntimeMeta, source string, now time.Time) (bool, string) {
	if runtime == nil {
		return false, "runtime missing"
	}
	now = now.UTC()
	budget := &runtime.Budget
	resetBudgetWindowIfNeeded(budget, now)
	if budget.DailyModelCallLimit > 0 && budget.DailyModelCalls >= budget.DailyModelCallLimit {
		reason := fmt.Sprintf("daily model call budget exhausted for %s (%d/%d)", source, budget.DailyModelCalls, budget.DailyModelCallLimit)
		budget.LastBlockedAt = now
		budget.LastBlockedReason = reason
		return false, reason
	}
	if budget.DailyModelCostLimit > 0 && budget.DailyModelCost >= budget.DailyModelCostLimit {
		reason := fmt.Sprintf("daily model cost budget exhausted for %s (%.6f/%.6f)", source, budget.DailyModelCost, budget.DailyModelCostLimit)
		budget.LastBlockedAt = now
		budget.LastBlockedReason = reason
		return false, reason
	}
	return true, ""
}

func recordModelBudgetUsage(runtime *agent.RuntimeMeta, cost float64, currency string, now time.Time) {
	if runtime == nil {
		return
	}
	budget := &runtime.Budget
	resetBudgetWindowIfNeeded(budget, now.UTC())
	budget.DailyModelCalls++
	if cost > 0 {
		budget.DailyModelCost += cost
	}
	if currency != "" {
		budget.ModelCostCurrency = currency
	}
}

func resetBudgetWindowIfNeeded(budget *agent.RuntimeBudgetMeta, now time.Time) {
	if budget == nil {
		return
	}
	window := budgetWindowStart(now)
	if budget.WindowStartedAt.IsZero() || !budget.WindowStartedAt.Equal(window) {
		budget.WindowStartedAt = window
		budget.DailyWakeups = 0
		budget.DailyModelCalls = 0
		budget.DailyModelCost = 0
		budget.ModelCostCurrency = ""
	}
}

func budgetWindowStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
