package control

import (
	"errors"
	"log/slog"
	"reasonix/internal/state/sessionstore"
)

// persistSessionSnapshot writes path with snapshot semantics, escalating to an
// owned rewrite when mid-turn reshape or same-revision divergence is detected.
// Authority-bound rewrites stay on the canonical path; missing/stale authority
// is returned as a typed error so callers rebind instead of forking recovery.
func persistSessionSnapshot(s *sessionstore.Session, path string, forceRewrite bool) (error, bool) {
	if s == nil {
		return nil, forceRewrite
	}
	forceRewrite = forceRewrite || s.NeedsRewriteSave()
	if forceRewrite {
		return s.SaveRewrite(path), true
	}
	err := s.SaveSnapshot(path)
	if authoritySaveError(err) {
		return err, false
	}
	if !errors.Is(err, sessionstore.ErrSessionSnapshotConflict) {
		return err, false
	}
	// Auto-compaction may rewrite between the decision and the write.
	if s.NeedsRewriteSave() {
		return s.SaveRewrite(path), true
	}
	// Same-revision diverged: prefer rewrite over a recovery fork when this
	// session still holds a live write authority for path.
	if err2, ok := retrySameRevisionDivergedRewrite(s, path, err); ok {
		return err2, true
	}
	return err, false
}

func retrySameRevisionDivergedRewrite(s *sessionstore.Session, path string, err error) (error, bool) {
	kind, ok := sessionstore.SnapshotConflictKind(err)
	if !ok || kind != sessionstore.SessionSnapshotConflictDiverged {
		return err, false
	}
	var conflict *sessionstore.SessionSnapshotConflictError
	if !errors.As(err, &conflict) || conflict == nil || conflict.BaseRevision != conflict.DiskRevision {
		return err, false
	}
	// SaveRewrite itself requires digest ownership or a live authority; a
	// process lease alone cannot claim the current bytes.
	return s.SaveRewrite(path), true
}

func (c *Controller) snapshot(markActivity, forceRewrite, shutdownRecovery bool) error {
	_, err := c.snapshotWithDurability(markActivity, forceRewrite, shutdownRecovery)
	return err
}

// writeSessionUsageRecord persists the session's token accounting beside the
// transcript. It rides the session's own persistence point so the record names
// the session the transcript was written for and cannot drift onto another; a
// failure to write a diagnostic must not report as a failure to save the turn.
func (c *Controller) writeSessionUsageRecord(path string) {
	if err := c.goalUsageTee.writeUsageRecord(path); err != nil {
		slog.Warn("controller: session usage record", "err", err)
	}
}
