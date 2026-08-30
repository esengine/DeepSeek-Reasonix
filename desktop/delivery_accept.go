package main

// Bound to the persisted tab state, not the end-of-turn notice card: that card
// renders once per finished turn, so a session reopened while still awaiting
// delivery would otherwise have no exit (#9036).

// AcceptDelivery accepts the active tab's pending delivery checks.
func (a *App) AcceptDelivery() error {
	return a.AcceptDeliveryToTab("")
}

// AcceptDeliveryToTab clears one tab's awaiting-delivery state. It touches no
// other status, starts no turn, and is idempotent: accepting a tab that is not
// awaiting delivery succeeds and changes nothing.
func (a *App) AcceptDeliveryToTab(tabID string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		return a.workspaceNotReadyErr(nil)
	}
	a.mu.Lock()
	cleared := a.tabs[tab.ID] == tab && awaitingDelivery(tab)
	if cleared {
		tab.ActivityStatus = ""
	}
	a.mu.Unlock()
	if cleared {
		a.emitProjectTreeChanged()
	}
	return nil
}

// awaitingDelivery reports the state the accept action is bound to.
func awaitingDelivery(tab *WorkspaceTab) bool {
	return tab != nil && tab.ActivityStatus == topicStatusAwaitingDelivery
}
