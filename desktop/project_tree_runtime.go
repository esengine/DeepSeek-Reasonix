package main

import (
	"context"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type projectTreeRuntimeState struct {
	revision atomic.Uint64
	// activityAt records the last activity-status event per tab ID. The TTL
	// watchdog reaps a live status that sees no terminating event; the state
	// lives here (not on WorkspaceTab) to keep tabs.go within budget.
	watchdogOnce sync.Once
	activityMu   sync.Mutex
	activityAt   map[string]time.Time
}

// noteActivityStatus refreshes a tab's activity timestamp on every status
// event, even an unchanged one: the TTL watchdog measures silence, not how
// long a status has been displayed, so a long but active turn is never reaped.
func (s *projectTreeRuntimeState) noteActivityStatus(a *App, tabID string) {
	s.setActivityAt(tabID, time.Now())
	// The watchdog starts lazily with the first status event — before that
	// there is nothing to reap.
	s.watchdogOnce.Do(func() { a.watchTopicActivityStatus() })
}

func (s *projectTreeRuntimeState) setActivityAt(tabID string, at time.Time) {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	if s.activityAt == nil {
		s.activityAt = map[string]time.Time{}
	}
	s.activityAt[tabID] = at
}

func (s *projectTreeRuntimeState) activityAtSnapshot() map[string]time.Time {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	out := make(map[string]time.Time, len(s.activityAt))
	maps.Copy(out, s.activityAt)
	return out
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

func (a *App) emitRuntimeEvent(name string, payload ...any) {
	if a != nil && a.ctx != nil {
		a.runtimeEvents.Emit(a.ctx, name, payload...)
	}
}

const (
	// topicActivityStatusTTL bounds how long a live spinner status may go
	// without any turn event before it is treated as orphaned. A flat TTL is
	// used instead of a per-session P99: turn durations are not tracked
	// per session in this package, and simplicity wins (#8528/#8555/#8859).
	topicActivityStatusTTL = 10 * time.Minute
	// topicActivityReapInterval is how often the watchdog scans for orphans.
	topicActivityReapInterval = 30 * time.Second
)

// liveTopicActivityStatus reports statuses that must be terminated by a
// TurnDone event; if that event is lost the spinner would run forever.
// waiting_confirmation is excluded: it waits on the user, not on turn events.
func liveTopicActivityStatus(status string) bool {
	switch status {
	case topicStatusThinking, topicStatusStreaming:
		return true
	}
	return false
}

// watchTopicActivityStatus reaps live activity statuses that have seen no
// turn event for longer than the TTL — the missed-TurnDone safety net.
func (a *App) watchTopicActivityStatus() {
	a.goSafe("topicActivityWatchdog", func() {
		ticker := time.NewTicker(topicActivityReapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.bootContext().Done():
				return
			case now := <-ticker.C:
				a.reapStaleTopicActivityStatus(now)
			}
		}
	})
}

func (a *App) reapStaleTopicActivityStatus(now time.Time) {
	activityAt := a.projectTreeRuntime.activityAtSnapshot()
	a.mu.Lock()
	var reaped []string
	for _, tab := range a.runtimeTabsLocked() {
		if tab == nil || !liveTopicActivityStatus(tab.ActivityStatus) {
			continue
		}
		if at := activityAt[tab.ID]; at.IsZero() || now.Sub(at) < topicActivityStatusTTL {
			continue
		}
		reaped = append(reaped, tab.ID+":"+tab.ActivityStatus)
		tab.ActivityStatus = ""
	}
	a.mu.Unlock()
	if len(reaped) == 0 {
		return
	}
	slog.Warn("desktop: cleared stale topic activity status (no turn event within TTL)",
		"tabs", reaped, "ttl", topicActivityStatusTTL.String())
	a.emitProjectTreeRuntimeChangedWithLegacy()
}

// reconcileTabActivityStatus clears a live spinner status the session's
// controller does not corroborate — a TurnDone missed while the session was
// detached would otherwise spin forever after reopen. The controller query
// takes the controller's own lock, so it runs outside App.mu (the same lock
// order rule as catalogRuntimeSnapshots).
func (a *App) reconcileTabActivityStatus(tab *WorkspaceTab) bool {
	if a == nil || tab == nil {
		return false
	}
	a.mu.RLock()
	status := tab.ActivityStatus
	ctrl := tab.Ctrl
	a.mu.RUnlock()
	if ctrl == nil || !liveTopicActivityStatus(status) || ctrl.Running() {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if tab.ActivityStatus != status {
		// A new turn (or its TurnDone) landed while the locks were dropped.
		return false
	}
	tab.ActivityStatus = ""
	slog.Warn("desktop: cleared stale topic activity status on session open", "tab", tab.ID, "status", status)
	return true
}

// setTabActivityStatus records the project-tree status for a tab's in-flight
// turn and notes the event time for the TTL watchdog.
func (a *App) setTabActivityStatus(tabID, status string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	tab := a.tabByEventSinkIDLocked(tabID)
	if tab == nil {
		return false
	}
	a.projectTreeRuntime.noteActivityStatus(a, tab.ID)
	status = normalizeTopicStatus(status)
	if tab.ActivityStatus == status {
		return false
	}
	tab.ActivityStatus = status
	return true
}
