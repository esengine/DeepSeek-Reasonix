package main

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/sessioncatalog"
)

// Recovery-copy count sweep (#8525/#8750/#9109). Catalog v4 folds recovery
// lineages into one ordinary list row, so copies multiply on disk even though
// the sidebar shows one conversation. Once a lineage piles up
// recoveryCopySweepThreshold or more covered copies under one root, the sweep
// keeps the newest recoveryCopySweepKeep and moves the rest to the recoverable
// session trash (never hard-delete; restore via History). Only recovery_copy=1
// members qualify; coverage is re-proven under removal guards before every
// move, and diverged copies (unique content) are never auto-swept. The sweep
// runs from existing reconcile hooks and is gated by
// recovery_cleanup.auto_enabled (default on).
const (
	recoveryCopySweepThreshold = 3
	recoveryCopySweepKeep      = 1
)

// sweepExcessRecoveryCopies runs the count sweep for one reconciled directory.
func (a *App) sweepExcessRecoveryCopies(catalog *sessioncatalog.Catalog, target sessioncatalog.DirectoryTarget) {
	if !config.RecoveryCopyAutoCleanupEnabled() {
		return
	}
	a.sweepExcessRecoveryCopiesIn(catalog, target, time.Now(), agent.RecoveryGCStartupGracePeriod)
}

func (a *App) sweepExcessRecoveryCopiesIn(catalog *sessioncatalog.Catalog, target sessioncatalog.DirectoryTarget, now time.Time, grace time.Duration) int {
	if a == nil || catalog == nil || a.shuttingDown.Load() {
		return 0
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), sessionCatalogReadTimeout)
	groups, err := catalog.ListRecoveryGroups(ctx, target.Path)
	cancel()
	if err != nil {
		slog.Warn("desktop: list recovery lineages for copy sweep", "dir", target.Path, "err", err)
		return 0
	}
	moved := 0
	for _, group := range groups {
		moved += a.sweepRecoveryGroupCopies(group, now, grace)
	}
	if moved > 0 {
		slog.Info("desktop: moved excess recovery copies to the session trash", "dir", target.Path, "count", moved)
	}
	return moved
}

func (a *App) sweepRecoveryGroupCopies(group sessioncatalog.RecoveryGroup, now time.Time, grace time.Duration) int {
	copies := make([]sessioncatalog.SessionRecord, 0, len(group.Members))
	for _, member := range group.Members {
		// Only proven-covered copies are redundant; diverged leaves and the
		// canonical representative always stay.
		if member.RecoveryCopy && member.Path != group.CanonicalPath {
			copies = append(copies, member)
		}
	}
	if len(copies) < recoveryCopySweepThreshold {
		return 0
	}
	sort.SliceStable(copies, func(i, j int) bool {
		if copies[i].LastActivityAt != copies[j].LastActivityAt {
			return copies[i].LastActivityAt > copies[j].LastActivityAt
		}
		return copies[i].Path < copies[j].Path
	})
	moved := 0
	for _, copy := range copies[recoveryCopySweepKeep:] {
		if grace > 0 {
			// A fresh copy may still be part of an active conflict flow.
			info, err := os.Stat(copy.Path)
			if err != nil || now.Sub(info.ModTime()) < grace {
				continue
			}
		}
		if a.trashSweptRecoveryCopy(group, copy.Path) {
			moved++
		}
	}
	return moved
}

// trashSweptRecoveryCopy moves one redundant copy to the recoverable session
// trash under the same guards the explicit UI delete uses. Any coverage or
// liveness doubt skips the copy untouched; it can be retried by a later sweep.
func (a *App) trashSweptRecoveryCopy(group sessioncatalog.RecoveryGroup, path string) bool {
	defer a.lockRuntimeMutation("recovery-copy-sweep")()
	a.sessionRemovalMu.Lock()
	defer a.sessionRemovalMu.Unlock()
	if a.sessionOpenInAnyTab(path) || agent.SessionLeaseHeld(path) {
		return false
	}
	var err error
	if group.CanonicalPath != "" {
		err = agent.TrashRecoveryBranchCoveredBy(path, group.CanonicalPath, group.Directory)
	} else {
		err = agent.TrashCoveredRecoveryBranch(path, group.Directory)
	}
	if err != nil {
		slog.Debug("desktop: sweep skipped recovery copy", "path", path, "group", group.ID, "err", err)
		return false
	}
	a.removeSessionCatalogPath(path, "recovery_copy_sweep")
	slog.Info("desktop: swept excess recovery copy to trash", "path", path, "group", group.ID)
	return true
}
