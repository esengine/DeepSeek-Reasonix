package main

func (a *App) setTabReadOnly(tabID string, readOnly bool) {
	var terminalSessions []*terminalSession
	a.mu.Lock()
	tab := a.tabs[tabID]
	if tab == nil || (tab.ReadOnly == readOnly && (readOnly || !tab.Takeover.Spectator)) {
		a.mu.Unlock()
		return
	}
	if a.terminals != nil {
		if readOnly {
			// Close the creation gate and detach existing sessions before
			// exposing the tab as read-only. The process I/O cleanup happens
			// after App.mu is released.
			terminalSessions = a.terminals.detachForTab(tabID)
		} else {
			// Reopen the terminal gate before exposing the tab as writable. A
			// concurrent create must never observe writable App state while
			// the terminal manager still treats this tab as closed.
			a.terminals.reopenForTab(tabID)
		}
	}
	tab.ReadOnly = readOnly
	if !readOnly {
		tab.Takeover.Spectator = false
	}
	a.saveTabsLocked()
	a.mu.Unlock()
	if len(terminalSessions) > 0 {
		// Existing shells can keep modifying the workspace without renderer
		// input, so entering a read-only channel must terminate them as part of
		// the same capability transition.
		a.terminals.closeSessions(terminalSessions)
	}
}
