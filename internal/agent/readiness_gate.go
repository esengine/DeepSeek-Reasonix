package agent

import "reasonix/internal/taskcontract"

// readinessPauseActive reports whether an unmet final-readiness requirement may
// pause the turn and hand the user a recovery card.
//
// Delivery and closed-loop Goal/Plan turns pause on their readiness contract.
// Standard reports quality gaps in its completion summary and ends normally.
func (a *Agent) readinessPauseActive(check finalReadinessCheck) bool {
	if a == nil {
		return false
	}
	return a.turn.constraints.PolicyFloor == taskcontract.PolicyFloorDelivery ||
		a.closedLoopActive() || a.planContractSnapshot() != nil
}
