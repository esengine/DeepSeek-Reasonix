package agent

const keptUserTurnsTriggerFrac = 0.10 // user turns: share of the compaction trigger

// retentionBudget is what one kind of verbatim retention may spend: a share of
// the window, held to a share of the trigger. The tail already stops at half
// the trigger; window shares alone let retention and tail together pass it on a
// window much larger than the trigger, and every fold is then refused as still
// at or above it.
func (a *contextWindow) retentionBudget(windowFrac, triggerFrac float64) int {
	window := a.effectiveContextWindow()
	if window <= 0 {
		return keptUserTurnsFloorTokens
	}
	budget := int(float64(window) * windowFrac)
	if trigger := a.compactTrigger(); trigger > 0 {
		budget = min(budget, int(float64(trigger)*triggerFrac))
	}
	return max(1, budget)
}
