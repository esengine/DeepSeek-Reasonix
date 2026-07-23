package main

import (
	"log/slog"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// recoveryGCInterval bounds how often the background sweep repeats after the
// startup run. Recovery branches accumulate slowly (only on a save conflict),
// so a low frequency is plenty and keeps the disk scan off the hot path.
const recoveryGCInterval = 6 * time.Hour

// startRecoveryGC waits for tab restore to complete, runs one sweep, then
// repeats on an interval until the app context is cancelled. The wait is
// load-bearing: restoreOrBuildTabs populates a.tabs asynchronously, and a
// sweep against the pre-restore empty tab map would see every saved tab's
// session as closed — and DeleteSession's tab-list persistence would then
// overwrite desktop-tabs.json with that empty snapshot.
func (a *App) startRecoveryGC() {
	a.goSafe("recoveryGC", func() {
		select {
		case <-a.tabsRestoredSignal():
		case <-a.ctx.Done():
			return
		}
		a.sweepReclaimableRecoveryBranches()
		ticker := time.NewTicker(recoveryGCInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.sweepReclaimableRecoveryBranches()
			}
		}
	})
}

// sweepReclaimableRecoveryBranches trashes conflict-recovery branches that
// preserve nothing unique (their content is covered by a still-present parent
// session), were never continued on, sat idle past the grace period, and are
// not held by any runtime. Trashing — never hard deletion — keeps every swept
// branch recoverable from the session trash. Returns how many were reclaimed.
func (a *App) sweepReclaimableRecoveryBranches() int {
	return a.reclaimRecoveryBranchesIn(recoveryGCDirs(), time.Now())
}

func (a *App) reclaimRecoveryBranchesIn(dirs []string, now time.Time) int {
	// Safe Mode loads none of the saved tabs, so the liveness checks below
	// would see every normally-open session as closed and reclaim recovery
	// branches the user's real layout still owns. Recovery boots never GC.
	if config.SafeModeRequested() {
		return 0
	}
	reclaimed := 0
	for _, dir := range dirs {
		reclaimable, err := agent.ReclaimableRecoveryBranches(dir, now, agent.RecoveryGCGracePeriod)
		if err != nil {
			slog.Warn("desktop: scan reclaimable recovery branches", "dir", dir, "err", err)
			continue
		}
		for _, path := range reclaimable {
			// Re-check liveness right before disposal: the scan is a snapshot,
			// and the user may have opened the branch since. DeleteSession then
			// runs the full removal path (removal guard, runtime unbinding,
			// trash move), so even a miss here lands in the recoverable trash.
			if agent.SessionLeaseHeld(path) || a.sessionOpenInAnyTab(path) {
				continue
			}
			if err := a.DeleteSession(path); err != nil {
				slog.Warn("desktop: trash reclaimed recovery branch", "path", path, "err", err)
				continue
			}
			reclaimed++
		}
	}
	if reclaimed > 0 {
		slog.Info("desktop: moved redundant recovery branches to the session trash", "count", reclaimed)
		a.emitProjectTreeChanged()
	}
	return reclaimed
}

// recoveryGCDirs returns every session directory the desktop lists sessions
// from: the global desktop and legacy shared dirs plus each saved project's
// session dirs, deduplicated.
func recoveryGCDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		key := projectRootKey(dir)
		if dir == "" || seen[key] {
			return
		}
		seen[key] = true
		dirs = append(dirs, dir)
	}
	add(desktopSessionDir(globalWorkspaceRoot()))
	add(config.SessionDir())
	for _, project := range loadProjectsFile().Projects {
		if root := normalizeProjectRoot(project.Root); root != "" {
			add(desktopSessionDir(root))
			add(config.ProjectSessionDir(root))
		}
	}
	return dirs
}

// sessionOpenInAnyTab reports whether any tab's current session is path.
// Lease checks cover live runtimes; this additionally covers tabs that hold a
// session without a lease (read-only channel views).
func (a *App) sessionOpenInAnyTab(path string) bool {
	key := sessionRuntimeKey(path)
	if key == "" {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab == nil {
			continue
		}
		if sessionRuntimeKey(tab.currentSessionPath()) == key {
			return true
		}
	}
	return false
}
