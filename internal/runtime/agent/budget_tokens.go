package agent

import "reasonix/internal/contract/provider"

// A reported total and its input/output split overlap. Either can establish a
// larger known count, but adding them would charge the same tokens twice.
func budgetUsageTokens(usage *provider.Usage) uint64 {
	if usage == nil {
		return 0
	}
	split := uint64(max(0, usage.PromptTokens)) + uint64(max(0, usage.CompletionTokens))
	return max(split, uint64(max(0, usage.TotalTokens)))
}

func addBudgetTokens(used, delta uint64) uint64 {
	if next := used + delta; next >= used {
		return next
	}
	return ^uint64(0)
}
