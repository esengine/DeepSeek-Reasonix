package control

import "reasonix/internal/sessioninbox"

// hasPendingUserWork reads only already-owned state and an already open inbox.
// It never creates an inbox or holds a Controller lock while taking an
// Agent/Store lock. A busy or unreadable durable inbox conservatively yields to
// potential user work, which must win over an automatic Todo nudge.
func (c *Controller) hasPendingUserWork() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	canceling := c.canceling
	parked := len(c.parkedTurns) > 0
	executor := c.executor
	c.mu.Unlock()
	if canceling || parked {
		return true
	}
	if executor != nil && executor.HasUnappliedSteer() {
		return true
	}
	c.inbox.mu.Lock()
	store := c.inbox.store
	c.inbox.mu.Unlock()
	if store == nil {
		return false
	}
	snapshot, err := store.TryFreshSnapshot()
	if err != nil || snapshot.Readonly {
		return true
	}
	for _, item := range snapshot.Items {
		if item.RunID != "" && item.RunID != sessioninbox.ProcessRunID() {
			return true
		}
		switch item.State {
		case sessioninbox.StateQueued, sessioninbox.StateBlocked,
			sessioninbox.StateUncertain, sessioninbox.StateSteerAccepted:
			return true
		}
	}
	return false
}
