package main

import (
	"context"
	"sort"
	"strings"
	"sync/atomic"
)

type projectTreeRuntimeState struct {
	revision atomic.Uint64
}

func syncRuntimeWorkspaceRootSpelling(tab *WorkspaceTab, projects []desktopProject) bool {
	if tab == nil || tab.Scope != "project" {
		return false
	}
	i := projectIndexByRoot(projects, tab.WorkspaceRoot)
	if i < 0 || tab.WorkspaceRoot == projects[i].Root {
		return false
	}
	tab.WorkspaceRoot = projects[i].Root
	return true
}

// catalogRuntimeSnapshots copies runtime identity under App.mu, then lets all
// controller calls happen after the app lock is released. Controllers own
// their own locks and must never become part of the App.mu lock order.
func (a *App) catalogRuntimeSnapshots() []catalogRuntimeSnapshot {
	if a == nil {
		return []catalogRuntimeSnapshot{}
	}
	a.mu.RLock()
	snapshots := make([]catalogRuntimeSnapshot, 0, len(a.tabs)+len(a.detachedSessions))
	collect := func(tab *WorkspaceTab, open bool) {
		if tab == nil || strings.TrimSpace(tab.TopicID) == "" {
			return
		}
		snapshots = append(snapshots, catalogRuntimeSnapshot{
			scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot, topicID: tab.TopicID,
			sessionPath: tab.SessionPath, activity: tab.ActivityStatus, topicTitle: tab.TopicTitle,
			topicTitleSource: tab.topicTitleSource, ctrl: tab.Ctrl, open: open,
		})
	}
	for _, tab := range a.tabs {
		collect(tab, true)
	}
	for _, tab := range a.detachedSessions {
		collect(tab, false)
	}
	a.mu.RUnlock()
	return snapshots
}

// GetProjectTreeRuntimeSnapshot returns the complete in-memory runtime
// projection. The frontend subscribes first and then calls this method; the
// independent revision makes either arrival order deterministic.
func (a *App) GetProjectTreeRuntimeSnapshot() ProjectTreeRuntimeSnapshot {
	revision := uint64(0)
	if a != nil {
		revision = a.projectTreeRuntime.revision.Load()
	}
	return a.projectTreeRuntimeSnapshot(revision)
}

func (a *App) projectTreeRuntimeSnapshot(revision uint64) ProjectTreeRuntimeSnapshot {
	type runtimeGroup struct {
		scope         string
		workspaceRoot string
		snapshots     []catalogRuntimeSnapshot
	}
	groups := map[string]*runtimeGroup{}
	for _, snapshot := range a.catalogRuntimeSnapshots() {
		scope, root := normalizeDesktopTopicScope(snapshot.scope, snapshot.workspaceRoot)
		snapshot.scope, snapshot.workspaceRoot = scope, root
		key := topicSummaryKey(scope, root, snapshot.topicID)
		group := groups[key]
		if group == nil {
			group = &runtimeGroup{scope: scope, workspaceRoot: root}
			groups[key] = group
		}
		group.snapshots = append(group.snapshots, snapshot)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	topics := make([]ProjectRuntimeTopic, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		nodes, _ := a.runtimeProjectTopicNodes(group.scope, group.workspaceRoot, group.snapshots)
		if len(nodes) > 0 {
			topics = append(topics, ProjectRuntimeTopic{Scope: group.scope, WorkspaceRoot: group.workspaceRoot, Node: nodes[0]})
		}
	}
	return ProjectTreeRuntimeSnapshot{Revision: revision, Topics: topics}
}

func (a *App) attachExistingSessionRuntime(tab *WorkspaceTab, path string, wailsCtx context.Context) bool {
	attached := a.attachExistingSessionRuntimeCore(tab, path, wailsCtx)
	if attached {
		a.emitProjectTreeRuntimeChangedWithLegacy()
	}
	return attached
}

func (a *App) closeTab(tabID string, allowDetach bool) error {
	err := a.closeTabRuntime(tabID, allowDetach)
	if err == nil {
		a.emitProjectTreeRuntimeChangedWithLegacy()
	}
	return err
}

func (a *App) emitProjectTreeChangedEvent() {
	if a.projectTreeChangedHook != nil {
		a.projectTreeChangedHook()
		return
	}
	a.emitProjectTreeRuntimeChanged()
	a.emitRuntimeEvent("project-tree:changed")
}

func (a *App) emitProjectTreeRuntimeChanged() {
	if a == nil {
		return
	}
	revision := a.projectTreeRuntime.revision.Add(1)
	a.emitRuntimeEvent("project-tree:runtime-changed", a.projectTreeRuntimeSnapshot(revision))
}

// The tagged legacy event keeps the previous frontend usable for one release.
func (a *App) emitProjectTreeRuntimeChangedWithLegacy() {
	a.emitProjectTreeRuntimeChanged()
	a.emitRuntimeEvent("project-tree:changed", map[string]string{"reason": "runtime"})
}

// Runtime ownership changes need an immediate in-memory projection and an
// asynchronous catalog reconciliation. The tagged compatibility event keeps
// pre-runtime-projection frontends working without making current frontends
// invalidate and rebuild the whole resident tree before catalog v2 catches up.
func (a *App) emitProjectTreeRuntimeChangedWithCatalogRefresh() {
	a.requestProjectTreeCatalogRefresh()
	a.emitProjectTreeRuntimeChangedWithLegacy()
}

func (a *App) emitRuntimeEvent(name string, payload ...any) {
	if a != nil && a.ctx != nil {
		a.runtimeEvents.Emit(a.ctx, name, payload...)
	}
}
