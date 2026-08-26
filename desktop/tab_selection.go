package main

import "fmt"

// SetActiveTab switches the frontend's active tab. Restored remote shells
// reconnect only when activated.
func (a *App) SetActiveTab(tabID string) error {
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
		if revive {
			tab.state = "connecting"
		}
		hostID, workspace := tab.ref.HostID, tab.ref.Workspace
		a.remoteTabMu.Unlock()
		if revive {
			a.emitRemoteTabState(tabID, "connecting", "")
			a.goSafe("remoteTabServe", func() { a.bootstrapRemoteTab(tabID, hostID, workspace) })
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
