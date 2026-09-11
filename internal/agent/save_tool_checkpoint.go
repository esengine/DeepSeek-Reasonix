package agent

import (
	"crypto/sha256"
	"log/slog"

	"reasonix/internal/provider"
)

// SaveToolCheckpoint commits the canonical transcript, CAS revision and event
// index before execution continues. Listing and display indexes are derived;
// rebuilding them for every tool receipt belongs to the normal snapshot path.
func (s *Session) SaveToolCheckpoint(path string, rewrite bool) error {
	mode := sessionSaveToolCheckpoint
	if rewrite {
		// Rewrites invalidate old indexed prefixes and must publish that change.
		mode = sessionSaveRewrite
	}
	return s.saveObserved(path, mode)
}

func refreshCheckpointDisplayIndex(path string, msgs []provider.Message, digest [sha256.Size]byte, revision int64, appendFrom int, deferred bool) error {
	if deferred {
		return nil
	}
	return refreshSessionDisplayIndex(path, msgs, digest, revision, appendFrom)
}

func (s *Session) markCheckpointPersisted(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int, msgs []provider.Message, deferred bool) {
	if !deferred {
		s.markPersistedWithListing(path, digest, version, revision, rewriteVersion, msgs)
		return
	}
	s.setPersistedBaseline(path, digest, version, revision, true, true, rewriteVersion, msgs)
	s.mu.Lock()
	s.persisted.projectionPending = true
	s.mu.Unlock()
}

// Checkpoint modes share ordinary CAS and rewrite rules, but defer projections.
func (mode sessionSaveMode) defersProjection() bool {
	return mode == sessionSaveToolCheckpoint
}

func (mode sessionSaveMode) allowsOwnedRewrite() bool {
	return mode == sessionSaveRewrite || mode == sessionSaveRewriteCompact
}

func (s *Session) refreshPendingCheckpointProjection(path string, msgs []provider.Message, digest [sha256.Size]byte, revision int64, deferred bool) {
	state := s.persistState(path)
	if deferred || (!state.projectionPending && state.saveVerified) {
		return
	}
	if err := refreshSessionDisplayIndex(path, msgs, digest, revision, -1); err != nil {
		// Match normal saves: a derived index cannot invalidate a durable receipt.
		slog.Warn("session: keeping save after display index write failure", "path", path, "err", err)
	}
}

func (mode sessionSaveMode) eventReason() string {
	switch mode {
	case sessionSaveSnapshot, sessionSaveToolCheckpoint:
		return "snapshot"
	case sessionSaveRewrite:
		return "rewrite"
	case sessionSaveRewriteCompact:
		return "rewrite-compact"
	default:
		return "save"
	}
}
