package agent

import "reasonix/internal/taskcontract"

// readinessPauseActive reports whether an unmet final-readiness requirement may
// pause the turn and hand the user a recovery card.
//
// The floor alone decides this. The standard floor restores the pre-#8866
// balanced feel across every execution mode: the host still records the
// readiness audit, but a gap never stops the turn. Goal keeps driving because
// its state machine reads Agent.ReadinessResult directly rather than consuming
// this error, so suppressing the pause costs it no signal.
func (a *Agent) readinessPauseActive() bool {
	if a == nil {
		return false
	}
	return a.turn.constraints.PolicyFloor == taskcontract.PolicyFloorDelivery
}
