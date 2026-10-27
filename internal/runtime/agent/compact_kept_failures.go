package agent

import (
	"slices"

	"reasonix/internal/contract/provider"
)

const (
	keptFailuresWindowFrac  = 0.05
	keptFailuresTriggerFrac = 0.10
)

// keptFailuresBudget caps what retained failures may spend of a checkpoint. A
// retained failure rides into the next fold's region, so without its own bound
// the retention grows by each round's failures until the acceptance ceiling.
func (a *contextWindow) keptFailuresBudget() int {
	return a.retentionBudget(keptFailuresWindowFrac, keptFailuresTriggerFrac)
}

// keepFailuresWithinBudget retains the newest failed tool results that fit the
// budget; the rest fold into the digest, whose coverage guard still names them.
func (a *contextWindow) keepFailuresWithinBudget(region []provider.Message, keep []bool, failures []int) {
	budget := a.keptFailuresBudget()
	for _, i := range slices.Backward(failures) {
		cost := fixedTokenEstimate(a.keptForProjection(region[i]))
		if cost > budget {
			continue
		}
		keep[i] = true
		budget -= cost
	}
}
