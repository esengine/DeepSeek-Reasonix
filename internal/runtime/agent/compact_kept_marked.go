package agent

import (
	"slices"

	"reasonix/internal/contract/provider"
)

const (
	keptMarkedWindowFrac  = 0.15
	keptMarkedTriggerFrac = 0.20
)

// keptMarkedBudget caps what [[keep]] turns may spend of a checkpoint. A turn the
// person marked is held verbatim, and a kept turn rides into the next fold's
// region, so without its own bound the retention grows by every marked turn
// until the candidate cannot land under the trigger and the fold is refused.
func (a *contextWindow) keptMarkedBudget() int {
	return a.retentionBudget(keptMarkedWindowFrac, keptMarkedTriggerFrac)
}

// keepMarkedWithinBudget retains the newest marked turns that fit the budget and
// returns the rest, which fold into the digest and are counted.
func (a *contextWindow) keepMarkedWithinBudget(region []provider.Message, keep []bool, marked []int) (dropped, droppedTokens int) {
	budget := a.keptMarkedBudget()
	for _, i := range slices.Backward(marked) {
		cost := fixedTokenEstimate(region[i])
		if cost > budget {
			dropped++
			droppedTokens += cost
			continue
		}
		keep[i] = true
		budget -= cost
	}
	return dropped, droppedTokens
}
