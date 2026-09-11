package control

import (
	"errors"

	"reasonix/internal/agent"
)

// persistSessionSnapshot writes path with snapshot semantics, escalating to an
// owned rewrite when mid-turn reshape or same-revision divergence is detected.
// Authority-bound rewrites stay on the canonical path; missing/stale authority
// is returned as a typed error so callers rebind instead of forking recovery.
func persistSessionSnapshot(s *agent.Session, path string, forceRewrite bool) (error, bool) {
	return persistSessionSnapshotMode(s, path, forceRewrite, false)
}

func persistSessionSnapshotMode(s *agent.Session, path string, forceRewrite, checkpoint bool) (error, bool) {
	if s == nil {
		return nil, forceRewrite
	}
	forceRewrite = forceRewrite || s.NeedsRewriteSave()
	var err error
	if checkpoint {
		err = s.SaveToolCheckpoint(path, forceRewrite)
	} else if forceRewrite {
		return s.SaveRewrite(path), true
	} else {
		err = s.SaveSnapshot(path)
	}
	if authoritySaveError(err) {
		return err, forceRewrite
	}
	if !errors.Is(err, agent.ErrSessionSnapshotConflict) {
		return err, forceRewrite
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

func retrySameRevisionDivergedRewrite(s *agent.Session, path string, err error) (error, bool) {
	kind, ok := agent.SnapshotConflictKind(err)
	if !ok || kind != agent.SessionSnapshotConflictDiverged {
		return err, false
	}
	var conflict *agent.SessionSnapshotConflictError
	if !errors.As(err, &conflict) || conflict == nil || conflict.BaseRevision != conflict.DiskRevision {
		return err, false
	}
	// SaveRewrite itself requires digest ownership or a live authority; a
	// process lease alone cannot claim the current bytes.
	return s.SaveRewrite(path), true
}
