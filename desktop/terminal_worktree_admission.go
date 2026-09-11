package main

// WriteTerminalForTab holds the project runtime admission through the process
// write so a merge reservation cannot begin between target resolution and the
// command becoming observable in the workspace.
func (a *App) WriteTerminalForTab(tabID, sessionID, data string) error {
	target, err := a.terminalTargetForTab(tabID, true)
	if err != nil {
		return err
	}
	releaseAdmission, err := a.beginWorkspaceRuntimeAdmission(target.workspaceRoot)
	if err != nil {
		return err
	}
	defer releaseAdmission()
	if a.terminals == nil {
		return errTerminalManagerOff
	}
	return a.terminals.write(target.workspaceKey, sessionID, []byte(data))
}

func (m *terminalManager) hasRunningForTabs(tabIDs map[string]struct{}) bool {
	if m == nil || len(tabIDs) == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range m.sessions {
		if session == nil || !session.view.Running {
			continue
		}
		if _, ok := tabIDs[session.tabID]; ok {
			return true
		}
	}
	return false
}
