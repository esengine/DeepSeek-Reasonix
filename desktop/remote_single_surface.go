package main

import (
	"context"
	"fmt"
)

// keepOnlyRemoteVisibleTab applies the workbench/creation one-surface policy
// after a remote tab has been registered. Local controllers with active work
// are detached exactly like local topic activation; inactive local bindings
// are closed, and other remote views drop only their event pumps while their
// managed Serve processes remain available in the project tree.
//
// The caller holds singleSurfaceMu, so local and remote navigation cannot add
// another visible surface while the snapshots and registry prune are in flight.
func (a *App) keepOnlyRemoteVisibleTab(tabID string) (TabMeta, error) {
	type localCandidate struct {
		id  string
		tab *WorkspaceTab
	}

	meta, remoteCancels, err := func() (TabMeta, []context.CancelFunc, error) {
		defer a.lockRuntimeMutation("prune-visible-tabs-for-remote")()
		a.sessionRemovalMu.Lock()
		defer a.sessionRemovalMu.Unlock()

		a.mu.Lock()
		candidates := make([]localCandidate, 0, len(a.tabs))
		for id, tab := range a.tabs {
			candidates = append(candidates, localCandidate{id: id, tab: tab})
		}
		a.mu.Unlock()

		// Snapshot while bindings remain visible and without a.mu: controller
		// recovery can re-enter App. sessionRemovalMu keeps destructive session
		// operations away from the same files until the prune commits.
		for _, candidate := range candidates {
			if err := a.snapshotTab(candidate.tab); err != nil {
				return TabMeta{}, nil, fmt.Errorf("save current session before switching tabs: %w", err)
			}
			if err := a.saveTabSessionMetaForCurrentSession(candidate.tab); err != nil {
				return TabMeta{}, nil, fmt.Errorf("save current session metadata before switching tabs: %w", err)
			}
		}

		a.mu.Lock()
		for _, candidate := range candidates {
			if a.tabs[candidate.id] != candidate.tab {
				a.mu.Unlock()
				return TabMeta{}, nil, fmt.Errorf("visible tabs changed while switching; retry")
			}
		}

		a.remoteTabMu.Lock()
		target := a.remoteTabs[tabID]
		if target == nil {
			a.remoteTabMu.Unlock()
			a.mu.Unlock()
			return TabMeta{}, nil, fmt.Errorf("remote tab %q closed while opening", tabID)
		}
		remoteCancels := make([]context.CancelFunc, 0, len(a.remoteTabs)-1)
		for id, tab := range a.remoteTabs {
			if id == tabID {
				continue
			}
			if tab.cancel != nil {
				remoteCancels = append(remoteCancels, tab.cancel)
			}
			delete(a.remoteTabs, id)
		}
		a.remoteTabLayout.activeID = tabID
		a.remoteTabLayout.order = []string{tabID}
		a.remoteTabLayout.stripOrder = []string{tabID}
		meta := remoteTabMetaLocked(target)
		a.remoteTabMu.Unlock()

		removedLocal := make([]*WorkspaceTab, 0, len(candidates))
		for _, candidate := range candidates {
			tab := candidate.tab
			if tab == nil || a.tabs[candidate.id] != tab {
				continue
			}
			if tab.Ctrl == nil || !tab.hasActiveRuntimeWork() {
				a.markTabRemovedLocked(tab)
			}
			removedLocal = append(removedLocal, tab)
			delete(a.tabs, candidate.id)
		}
		a.activeTabID = ""
		a.tabOrder = nil
		dir, entries, activeID, version := a.saveTabsCollectLocked()
		a.mu.Unlock()

		for _, tab := range removedLocal {
			a.removeVisibleTabRuntimeAdmissionHeld(tab)
		}
		a.saveTabsWrite(dir, entries, activeID, version)
		return meta, remoteCancels, nil
	}()
	if err != nil {
		return TabMeta{}, err
	}
	for _, cancel := range remoteCancels {
		cancel()
	}
	a.emitProjectTreeRuntimeChangedWithLegacy()
	return meta, nil
}
