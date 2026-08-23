package agent

import "reasonix/internal/taskcontract"

// readinessPauseActive reports whether an unmet final-readiness requirement may
// pause the turn and hand the user a recovery card.
//
// Delivery pauses on its complete readiness contract. Standard pauses only for
// the controller-owned task-progress projection (requested mutation/current
// task todo); verification, review, and sign-off remain completion attention.
func (a *Agent) readinessPauseActive(check finalReadinessCheck) bool {
	if a == nil {
		return false
	}
	if a.turn.constraints.PolicyFloor == taskcontract.PolicyFloorDelivery {
		return true
	}
	return check.continuationClass() == ReadinessContinuationTaskProgress
}
