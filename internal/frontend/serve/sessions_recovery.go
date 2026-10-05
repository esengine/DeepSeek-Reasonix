package serve

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"time"
)

// recoveryGCInterval bounds how often the background sweep repeats after the
// startup run, matching the desktop host.
const recoveryGCInterval = 6 * time.Hour

// hiddenRecoveryCopy reports a conflict-recovery fork that preserves nothing
// its parent lacks. A save-conflict loop mints one per save, so listing them
// fills the sidebar with identical-looking copies of one conversation — the
// desktop catalog and the CLI picker each fold them, serve listed every one.
// The session in use is never hidden, and coverage is proven from content.
func hiddenRecoveryCopy(info sessionstore.SessionInfo, current string) bool {
	if !info.Recovered || sessionstore.CanonicalSessionPath(info.Path) == current {
		return false
	}
	return sessionstore.RecoveryBranchCoveredByParent(info.Path, filepath.Dir(info.Path))
}

// StartRecoveryGC runs the recovery hygiene the CLI and desktop hosts already
// do: covered, idle, unleased forks move to the recoverable session trash so a
// past conflict loop stops occupying disk. serve had no sweep at all, so every
// fork a studio window ever made stayed forever. Returns immediately.
func (s *Server) StartRecoveryGC(ctx context.Context) {
	go func() {
		startup := time.NewTimer(sessionstore.RecoveryGCStartupGracePeriod)
		defer startup.Stop()
		select {
		case <-ctx.Done():
			return
		case <-startup.C:
		}
		// Protect an upgrading user for a full startup grace before the first
		// sweep, then clear the backlog without waiting out the long interval.
		s.sweepRecoveryBranches(sessionstore.RecoveryGCStartupGracePeriod)
		ticker := time.NewTicker(recoveryGCInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweepRecoveryBranches(sessionstore.RecoveryGCGracePeriod)
			}
		}
	}()
}

// sweepRecoveryBranches trashes every recovery branch in the session directory
// that is provably redundant, never continued on, unleased, and idle past
// grace. Trashing keeps each one restorable. Returns how many moved.
func (s *Server) sweepRecoveryBranches(grace time.Duration) int {
	dir := s.ctl().SessionDir()
	if dir == "" {
		return 0
	}
	candidates, err := sessionstore.ReclaimableRecoveryBranches(dir, time.Now(), grace)
	if err != nil {
		slog.Warn("serve: scan reclaimable recovery branches", "dir", dir, "err", err)
		return 0
	}
	current := sessionstore.CanonicalSessionPath(s.ctl().SessionPath())
	swept := 0
	for _, path := range candidates {
		// The lease check inside the scan already covers a live runtime; this
		// also covers a controller holding the branch without one.
		if sessionstore.CanonicalSessionPath(path) == current {
			continue
		}
		// Coverage is re-proven under removal guards here, so a branch someone
		// continued on between the scan and now is left alone.
		if err := sessionstore.TrashCoveredRecoveryBranch(path, dir); err != nil {
			slog.Warn("serve: trash redundant recovery branch", "path", path, "err", err)
			continue
		}
		swept++
	}
	if swept > 0 {
		slog.Info("serve: moved redundant recovery branches to the session trash", "count", swept)
	}
	return swept
}

// removeSessionVersions removes the earlier versions of the session at abs: a
// version nobody lists must not outlive its conversation.
func removeSessionVersions(absDir, abs string) error {
	var errs error
	for _, version := range sessionstore.SessionVersionPaths(abs) {
		errs = errors.Join(errs, removeSessionFiles(absDir, version))
	}
	return errs
}
