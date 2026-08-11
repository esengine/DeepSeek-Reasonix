package main

import (
	"errors"
	"log/slog"

	"reasonix/internal/agent"
)

func (a *App) captureTopicRuntimeBindings(topicID string) []removedSessionRuntime {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var captured []removedSessionRuntime
	for _, tabs := range []map[string]*WorkspaceTab{a.tabs, a.detachedSessions} {
		for _, tab := range tabs {
			if tab == nil || tab.TopicID != topicID {
				continue
			}
			captured = append(captured, removedRuntimeFromTab(tab, tabRuntimeSessionDir(tab), canonicalTabSessionPath(tab.currentSessionPath())))
		}
	}
	return captured
}

// snapshotTopicRuntimeBindings keeps bindings usable until every writable
// controller snapshots. The caller owns runtime admission, blocking new turns.
func (a *App) snapshotTopicRuntimeBindings(captured []removedSessionRuntime) error {
	for _, item := range captured {
		if item.ctrl == nil || item.readOnly {
			continue
		}
		if item.ctrl.Running() {
			return errTopicHasActiveWork
		}
		if err := item.ctrl.Snapshot(); err != nil {
			if !errors.Is(err, agent.ErrSessionSnapshotConflict) {
				return err
			}
			kind, _ := agent.SnapshotConflictKind(err)
			slog.Warn("desktop: skipping stale runtime snapshot before removing topic", "conflict_kind", kind)
		}
	}
	return nil
}

func topicRuntimeBindingsMatchLocked(tabs map[string]*WorkspaceTab, topicID string, expected map[*WorkspaceTab]removedSessionRuntime) (int, bool) {
	matched := 0
	for _, tab := range tabs {
		if tab == nil || tab.TopicID != topicID {
			continue
		}
		item, ok := expected[tab]
		if !ok || tab.Ctrl != item.ctrl || tabRuntimeSessionDir(tab) != item.sessionDir || canonicalTabSessionPath(tab.currentSessionPath()) != item.sessionPath {
			return 0, false
		}
		matched++
	}
	return matched, true
}

// removeTopicRuntimeBindingsIfUnchanged commits only when the captured runtime
// generation still matches, so stale completion cannot remove a rebound tab.
func (a *App) removeTopicRuntimeBindingsIfUnchanged(topicID string, captured []removedSessionRuntime) (fallbackRuntimeTarget, bool) {
	expected := make(map[*WorkspaceTab]removedSessionRuntime, len(captured))
	for _, item := range captured {
		expected[item.tab] = item
	}
	a.mu.Lock()
	visible, visibleOK := topicRuntimeBindingsMatchLocked(a.tabs, topicID, expected)
	detached, detachedOK := topicRuntimeBindingsMatchLocked(a.detachedSessions, topicID, expected)
	if !visibleOK || !detachedOK || visible+detached != len(captured) {
		a.mu.Unlock()
		return fallbackRuntimeTarget{}, false
	}
	var fallback fallbackRuntimeTarget
	fallbackSet := false
	for id, tab := range a.tabs {
		if _, ok := expected[tab]; !ok {
			continue
		}
		if !fallbackSet {
			fallback = fallbackRuntimeTarget{scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot}
			fallbackSet = true
		}
		a.markTabRemovedLocked(tab)
		delete(a.tabs, id)
		a.removeTabOrderLocked(id)
		if a.activeTabID == id {
			a.activeTabID = ""
		}
	}
	for key, tab := range a.detachedSessions {
		if _, ok := expected[tab]; !ok {
			continue
		}
		if !fallbackSet {
			fallback = fallbackRuntimeTarget{scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot}
			fallbackSet = true
		}
		a.markTabRemovedLocked(tab)
		delete(a.detachedSessions, key)
	}
	if a.activeTabID == "" && len(a.tabOrder) > 0 {
		a.activeTabID = a.tabOrder[0]
	}
	fallback.needs = len(captured) > 0 && len(a.tabs) == 0
	dir, entries, activeID, version := a.saveTabsCollectLocked()
	a.mu.Unlock()
	a.saveTabsWrite(dir, entries, activeID, version)
	return fallback, true
}

func (a *App) finalizeRemovedTopicRuntimes(removed []removedSessionRuntime) {
	for _, item := range removed {
		if item.sink != nil {
			item.sink.clearContext()
		}
		if item.ctrl == nil || item.readOnly {
			continue
		}
		item.ctrl.SetSessionPath("")
		a.quiesceTabAutosave(item.tab)
	}
}

func (a *App) tryLockRuntimeMutation(operation string) (func(), bool) {
	if hook := a.runtimeMutationBeforeLockHook; hook != nil {
		hook(operation)
	}
	if !a.runtimeRebuildMu.TryLock() {
		return nil, false
	}
	if !a.runtimeAdmissionMu.TryLock() {
		a.runtimeRebuildMu.Unlock()
		return nil, false
	}
	return func() {
		a.runtimeAdmissionMu.Unlock()
		a.runtimeRebuildMu.Unlock()
	}, true
}
