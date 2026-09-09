package control

import (
	"errors"
	"fmt"
	"log/slog"

	"reasonix/internal/agent"
)

// recoverShutdownSave answers a save lock that stayed held for the whole
// bounded wait during the shutdown snapshot. A schema-2 log takes the
// unsaved tail without the lock and the controller stays on its path; a
// schema-1 session still gets a distinct recovery copy.
func (c *Controller) recoverShutdownSave(s *agent.Session, path string, saveErr error, forceRewrite bool) (string, error) {
	handled, err := s.AppendForShutdownWithoutLock(path, forceRewrite)
	if err != nil {
		return "", fmt.Errorf("append session tail at shutdown: %w", err)
	}
	if !handled {
		return c.recoverShutdownSnapshot(path, saveErr)
	}
	appendSnapshotConflictDiagnostic(path, "shutdown", "appended_without_lock", saveErr, "", false)
	slog.Warn("controller: shutdown snapshot lock timed out; appended the unsaved tail to the session log without it", "path", path)
	return path, nil
}

// listingDeferredAfterUnlockedAppend accepts a listing-sidecar refresh that
// failed on the very lock the shutdown append just worked around; the next
// locked save refreshes the sidecar along with the other derived files.
func listingDeferredAfterUnlockedAppend(s *agent.Session, path string, err error) bool {
	return errors.Is(err, agent.ErrSessionFileLockHeld) && s.DerivedFilesPending(path)
}
