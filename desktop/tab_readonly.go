package main

func (a *App) setTabReadOnly(tabID string, readOnly bool) {
	a.setTabReadOnlyWithTerminalPolicy(tabID, readOnly, false)
}

// setTabReadOnlyPreservingTerminals revokes the tab's session writer without
// treating that session handoff as a workspace-terminal capability change. A
// CLI running in the integrated terminal must survive so it can switch to a
// different session after the current one is reclaimed.
func (a *App) setTabReadOnlyPreservingTerminals(tabID string, readOnly bool) {
	a.setTabReadOnlyWithTerminalPolicy(tabID, readOnly, true)
}

func (a *App) setTabReadOnlyWithTerminalPolicy(tabID string, readOnly, preserveTerminals bool) {
	var terminalSessions []*terminalSession
	a.mu.Lock()
	tab := a.tabs[tabID]
	if tab == nil || (tab.ReadOnly == readOnly && (readOnly || !tab.Takeover.Spectator)) {
		a.mu.Unlock()
		return
	}
	if a.terminals != nil {
		if readOnly {
			if !preserveTerminals {
				// Close the creation gate and detach existing sessions before
				// exposing the tab as read-only. The process I/O cleanup happens
				// after App.mu is released.
				terminalSessions = a.terminals.detachForTab(tabID)
			}
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
