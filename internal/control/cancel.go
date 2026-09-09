package control

import "reasonix/internal/event"

// Cancel aborts the in-flight turn. A goroutine blocked awaiting approval
// unblocks via the cancelled context.
func (c *Controller) Cancel() {
	c.promptResolveMu.Lock()
	turnID, cancelled := c.cancelTurnLocked()
	c.promptResolveMu.Unlock()
	c.finishCancel(turnID, cancelled)
}

// cancelLocked is Cancel for callers that already hold promptResolveMu.
func (c *Controller) cancelLocked() {
	turnID, cancelled := c.cancelTurnLocked()
	c.finishCancel(turnID, cancelled)
}

// cancelTurnLocked signals the turn before any observable work: the status
// emit that follows is a synchronous event barrier, and a stalled event lane
// must never keep the provider stream or a tool process alive after Stop.
func (c *Controller) cancelTurnLocked() (string, bool) {
	c.mu.Lock()
	cancel := c.cancel
	if cancel != nil {
		c.canceling = true
	}
	c.mu.Unlock()
	if cancel == nil {
		return "", false
	}
	turnID := ""
	if ledger := c.turnEventLedger(); ledger != nil {
		turnID = ledger.ActiveTurnID()
	}
	cancel()
	c.promptOwner.CancelAll()
	c.approval.clearAll()
	return turnID, true
}

func (c *Controller) finishCancel(turnID string, cancelled bool) {
	defer c.refreshRuntimeState(event.Event{})
	if cancelled {
		c.emitTurnStatus(event.TurnCancelling, turnID)
		return
	}
	if c.goals.active() {
		c.stopGoal(GoalStatusStopped)
	}
}
