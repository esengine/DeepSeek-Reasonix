package control

// A team-delivered turn runs under its sender chain's approval ceiling: a
// message grants nothing, so the receiver does no more unattended than the
// strictest session that handed the work on. The approval manager owns it.

// effectiveApprovalMode is the posture tools are gated under now.
func (c *Controller) effectiveApprovalMode() string {
	return c.approval.effectiveMode()
}

// holdTurnCeiling applies ceiling to the turn about to run and to the
// sub-agents it starts. One no stricter than the posture changes nothing.
func (c *Controller) holdTurnCeiling(ceiling string) {
	if ceiling != "" && c.approval.holdCeiling(ceiling) {
		c.refreshTurnGates()
	}
}

// releaseTurnCeiling lifts the turn's ceiling once it has ended.
func (c *Controller) releaseTurnCeiling() {
	if c.approval.releaseCeiling() {
		c.refreshTurnGates()
	}
}

func (c *Controller) refreshTurnGates() {
	if c.subagentGate != nil {
		c.subagentGate.UpdateAttended(c.effectiveApprovalMode())
	}
	c.refreshInteractiveGate()
}
