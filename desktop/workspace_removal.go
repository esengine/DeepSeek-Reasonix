package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"reasonix/desktop/internal/workspacestate"
)

type workspaceRemovalFlight struct {
	done chan struct{}
	err  error
}

type workspaceRemovalTiming struct {
	started      time.Time
	runtimeLock  time.Duration
	removalLock  time.Duration
	snapshot     time.Duration
	runtimeClose time.Duration
	fallback     time.Duration
}

type workspaceTabCandidate struct {
	id  string
	tab *WorkspaceTab
}

type workspaceRuntimeClosure struct {
	tabs     []*WorkspaceTab
	detached []*WorkspaceTab
	fallback *WorkspaceTab
}

func (a *App) RemoveWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("workspace path is required")
	}
	if a.shuttingDown.Load() {
		return fmt.Errorf("application is shutting down")
	}
	dir = normalizeProjectRoot(dir)
	a.workspaceRemovalMu.Lock()
	if a.workspaceRemovalFlights == nil {
		a.workspaceRemovalFlights = map[string]*workspaceRemovalFlight{}
	}
	if flight := a.workspaceRemovalFlights[dir]; flight != nil {
		a.workspaceRemovalMu.Unlock()
		if hook := a.workspaceRemovalFlightJoinHook; hook != nil {
			hook(dir)
		}
		<-flight.done
		return flight.err
	}
	flight := &workspaceRemovalFlight{done: make(chan struct{})}
	a.workspaceRemovalFlights[dir] = flight
	a.workspaceRemovalMu.Unlock()

	flight.err = a.removeWorkspace(dir)
	a.workspaceRemovalMu.Lock()
	delete(a.workspaceRemovalFlights, dir)
	close(flight.done)
	a.workspaceRemovalMu.Unlock()
	return flight.err
}

func (a *App) removeWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("workspace path is required")
	}
	dir = normalizeProjectRoot(dir)
	if a.shuttingDown.Load() {
		return fmt.Errorf("application is shutting down")
	}
	timing := workspaceRemovalTiming{started: time.Now()}
	defer logWorkspaceRemoval(dir, &timing)

	fallback, err := a.removeWorkspaceRuntimes(dir, &timing)
	if err != nil {
		return err
	}
	if hook := a.lifecycleCheckpointHook; hook != nil {
		hook("remove-workspace-before-fallback")
	}
	if fallback != nil && !a.shuttingDown.Load() {
		started := time.Now()
		a.startTabControllerBuild(fallback)
		timing.fallback = time.Since(started)
	}
	if err := removeWorkspaceRegistration(dir); err != nil {
		return err
	}
	a.emitProjectTreeMetadataChanged()
	return nil
}

func logWorkspaceRemoval(dir string, timing *workspaceRemovalTiming) func() {
	return func() {
		slog.Info("desktop: remove workspace completed", "workspace", dir,
			"total_ms", time.Since(timing.started).Milliseconds(),
			"runtime_lock_ms", timing.runtimeLock.Milliseconds(),
			"removal_lock_ms", timing.removalLock.Milliseconds(),
			"snapshot_ms", timing.snapshot.Milliseconds(),
			"runtime_close_ms", timing.runtimeClose.Milliseconds(),
			"fallback_ms", timing.fallback.Milliseconds())
	}
}

func (a *App) removeWorkspaceRuntimes(dir string, timing *workspaceRemovalTiming) (*WorkspaceTab, error) {
	started := time.Now()
	releaseRuntime := a.lockRuntimeMutation("remove-workspace")
	timing.runtimeLock = time.Since(started)
	defer releaseRuntime()
	if a.shuttingDown.Load() {
		return nil, fmt.Errorf("application is shutting down")
	}

	started = time.Now()
	a.sessionRemovalMu.Lock()
	timing.removalLock = time.Since(started)
	defer a.sessionRemovalMu.Unlock()

	candidates, err := a.workspaceRemovalCandidates(dir)
	if err != nil {
		return nil, err
	}
	started = time.Now()
	snapshotted, err := a.snapshotWorkspaceCandidates(dir, candidates)
	timing.snapshot = time.Since(started)
	if err != nil {
		return nil, err
	}
	if err := a.hideWorkspace(dir); err != nil {
		return nil, err
	}
	closure, err := a.detachWorkspaceRuntimes(dir, candidates, snapshotted)
	if err != nil {
		return nil, err
	}
	started = time.Now()
	a.closeWorkspaceRuntimes(closure)
	timing.runtimeClose = time.Since(started)
	return closure.fallback, nil
}

func (a *App) workspaceRemovalCandidates(dir string) ([]workspaceTabCandidate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.workspaceHasActiveRuntimeWorkLocked(dir) {
		return nil, fmt.Errorf("workspace has running sessions; stop them before removing")
	}
	candidates := make([]workspaceTabCandidate, 0)
	for id, tab := range a.tabs {
		if tabInWorkspace(tab, dir) {
			candidates = append(candidates, workspaceTabCandidate{id: id, tab: tab})
		}
	}
	return candidates, nil
}

func (a *App) workspaceHasActiveRuntimeWorkLocked(dir string) bool {
	for _, tab := range a.tabs {
		if tabInWorkspace(tab, dir) && tab.hasActiveRuntimeWork() {
			return true
		}
	}
	for _, tab := range a.detachedSessions {
		if tabInWorkspace(tab, dir) && tab.hasActiveRuntimeWork() {
			return true
		}
	}
	return false
}

func (a *App) snapshotWorkspaceCandidates(dir string, candidates []workspaceTabCandidate) (map[string]*WorkspaceTab, error) {
	snapshotted := make(map[string]*WorkspaceTab, len(candidates))
	for _, candidate := range candidates {
		snapshotted[candidate.id] = candidate.tab
		if err := a.snapshotTab(candidate.tab); err != nil {
			slog.Warn("desktop: snapshot before removing workspace failed", "tab", candidate.id, "workspace", dir, "err", err)
			return nil, fmt.Errorf("save current session before removing workspace: %w", err)
		}
	}
	return snapshotted, nil
}

func (a *App) hideWorkspace(dir string) error {
	err := a.workspaceRegistry().SetWorkspaceVisible(a.bootContext(), desktopWorkspaceID("project", dir), false)
	if errors.Is(err, workspacestate.ErrWorkspaceNotFound) {
		return nil
	}
	return err
}

func (a *App) detachWorkspaceRuntimes(dir string, candidates []workspaceTabCandidate, snapshotted map[string]*WorkspaceTab) (workspaceRuntimeClosure, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var closure workspaceRuntimeClosure
	if a.workspaceHasActiveRuntimeWorkLocked(dir) {
		return closure, fmt.Errorf("workspace has running sessions; stop them before removing")
	}
	if a.workspaceTabsChangedLocked(dir, snapshotted) {
		return closure, fmt.Errorf("workspace tabs changed while removing; retry")
	}
	for _, candidate := range candidates {
		if candidate.tab == nil || a.tabs[candidate.id] != candidate.tab || !tabInWorkspace(candidate.tab, dir) {
			continue
		}
		a.markTabRemovedLocked(candidate.tab)
		closure.tabs = append(closure.tabs, candidate.tab)
		delete(a.tabs, candidate.id)
		a.removeTabOrderLocked(candidate.id)
		if a.activeTabID == candidate.id {
			a.activeTabID = ""
		}
	}
	for key, tab := range a.detachedSessions {
		if tabInWorkspace(tab, dir) {
			closure.detached = append(closure.detached, tab)
			delete(a.detachedSessions, key)
		}
	}
	closure.fallback = a.ensureWorkspaceFallbackLocked()
	a.saveTabsLocked()
	return closure, nil
}

func (a *App) workspaceTabsChangedLocked(dir string, snapshotted map[string]*WorkspaceTab) bool {
	for id, tab := range a.tabs {
		if tabInWorkspace(tab, dir) && snapshotted[id] != tab {
			return true
		}
	}
	return false
}

func (a *App) ensureWorkspaceFallbackLocked() *WorkspaceTab {
	if len(a.tabs) == 0 && !a.shuttingDown.Load() {
		fallback := a.createTabEntry("global", globalTabWorkspaceRoot(), "")
		fallback.TopicTitle = "Global"
		fallback.sink = &tabEventSink{tabID: fallback.ID, app: a, ctx: a.ctx}
		a.tabs[fallback.ID] = fallback
		a.tabOrder = append(a.tabOrder, fallback.ID)
		a.activeTabID = fallback.ID
		return fallback
	}
	if a.activeTabID == "" {
		if ordered := a.orderedTabIDsLocked(); len(ordered) > 0 {
			a.activeTabID = ordered[0]
		}
	}
	return nil
}

func (a *App) closeWorkspaceRuntimes(closure workspaceRuntimeClosure) {
	for _, tab := range closure.tabs {
		a.closeTabRuntimeAdmissionHeld(tab)
	}
	for _, tab := range closure.detached {
		a.closeTabRuntimeAdmissionHeld(tab)
	}
}

func removeWorkspaceRegistration(dir string) error {
	forgetWorkspace(dir)
	if err := removeProject(dir); err != nil {
		return err
	}
	if loadWorkspace() != dir {
		return nil
	}
	remaining := loadProjectsFile()
	if len(remaining.Projects) > 0 {
		saveWorkspace(remaining.Projects[0].Root)
	} else {
		clearWorkspace()
	}
	return nil
}
