package main

import (
	"fmt"

	"reasonix/internal/control"
)

// activeOrSingleLocalTab resolves the workspace tab a local command should
// target: the active tab when there is one, otherwise the dormant tab a
// remote-only layout restores. The controller is read with the tab so a
// runtime built in between cannot look like a replaced one.
func (a *App) activeOrSingleLocalTab() (*WorkspaceTab, control.SessionAPI) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.tabByIDLocked(""); tab != nil {
		return tab, tab.Ctrl
	}
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			return tab, tab.Ctrl
		}
	}
	return nil, nil
}

// singleLocalTab resolves the workspace tab local commands should target when no
// tab is active. A remote-only single-surface layout restores one dormant tab
// for exactly this purpose, so callers resolve instead of reporting that the
// workspace is not ready.
func (a *App) singleLocalTab() *WorkspaceTab {
	tab, _ := a.activeOrSingleLocalTab()
	return tab
}

// SetActiveTab switches the frontend's active tab. Restored remote shells
// reconnect only when activated.
func (a *App) SetActiveTab(tabID string) error {
	// Even selecting the already-visible tab cancels a slower source adoption.
	a.desktopSessions.navigationSeq.Add(1)
	a.tabSelectionMu.Lock()
	defer a.tabSelectionMu.Unlock()

	a.remoteTabMu.Lock()
	if _, isRemote := a.remoteTabs[tabID]; isRemote {
		switchingFromLocal := a.remoteTabLayout.activeID == ""
		a.remoteTabMu.Unlock()
		if switchingFromLocal {
			if err := a.snapshotActiveLocalBeforeRemote(); err != nil {
				return err
			}
		}

		a.remoteTabMu.Lock()
		tab, isRemote := a.remoteTabs[tabID]
		if !isRemote {
			a.remoteTabMu.Unlock()
			return fmt.Errorf("tab %q not found", tabID)
		}
		a.remoteTabLayout.activeID = tabID
		revive := tab.state == "disconnected"
		terminalState, terminalErr := "", ""
		if revive {
			tab.state = "connecting"
		} else if tab.state == "error" || tab.state == "serve_down" {
			terminalState, terminalErr = tab.state, tab.err
		}
		hostID, workspace := tab.ref.HostID, tab.ref.Workspace
		a.remoteTabMu.Unlock()
		if revive {
			a.emitRemoteTabState(tabID, "connecting", "")
			a.goRemoteTabSafe("remoteTabServe", func() { a.bootstrapRemoteTab(tabID, hostID, workspace) })
		} else if terminalState != "" {
			// A restored shell can fail before its React surface subscribes. Re-publish
			// the authoritative terminal state on activation so the recovery UI does
			// not remain on an inferred connecting placeholder.
			a.emitRemoteTabState(tabID, terminalState, terminalErr)
		}
		a.saveTabsFromRemote()
		return nil
	}
	a.remoteTabMu.Unlock()
	a.mu.RLock()
	_, ok := a.tabs[tabID]
	alreadyActive := a.activeTabID == tabID
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("tab %q not found", tabID)
	}
	if alreadyActive {
		a.remoteTabMu.Lock()
		a.remoteTabLayout.activeID = ""
		a.remoteTabMu.Unlock()
		a.saveTabsFromRemote()
		return nil
	}
	a.mu.RLock()
	active := a.tabs[a.activeTabID]
	a.mu.RUnlock()
	if err := a.snapshotTabForAction(active, "switching tabs"); err != nil {
		return err
	}

	a.mu.Lock()
	if _, ok := a.tabs[tabID]; !ok {
		a.mu.Unlock()
		return fmt.Errorf("tab %q not found", tabID)
	}
	if a.activeTabID == tabID {
		a.mu.Unlock()
		a.remoteTabMu.Lock()
		a.remoteTabLayout.activeID = ""
		a.remoteTabMu.Unlock()
		a.saveTabsFromRemote()
		return nil
	}
	a.activeTabID = tabID
	next := a.tabs[tabID]
	// A tab restored dormant (remote-only layout) has no runtime yet: activating
	// it is the first demand for one.
	dormant := next != nil && next.Ctrl == nil
	// A direct click supersedes pending publication without cancelling its
	// build: the tab stays open, and selecting that same tab keeps it alive.
	supersededReq, supersededTab := a.supersedePendingTopicActivationLocked(tabID, false)
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.mu.Unlock()
	a.remoteTabMu.Lock()
	a.remoteTabLayout.activeID = ""
	a.remoteTabMu.Unlock()

	// I/O outside the lock — disk writes can block for hundreds of ms on
	// Windows when antivirus or the search indexer briefly locks the file.
	a.saveTabsWrite(dir, entries, activeID, version)
	if dormant {
		a.startTabControllerBuild(next)
	}
	if active != nil {
		active.clearRuntimeDisplayCurrency()
	}
	if next != nil {
		next.clearRuntimeDisplayCurrency()
	}
	if supersededReq != "" {
		a.emitTopicActivation(TopicActivationEvent{RequestID: supersededReq, TabID: supersededTab, Phase: topicActivationPhaseCancelled})
	}
	a.kickDeferredRebuildRetry()
	return nil
}
