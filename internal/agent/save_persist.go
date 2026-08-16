package agent

import (
	"crypto/sha256"
	"os"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func (s *Session) markPersisted(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int) {
	// Recovery-lane writes stay unpaired so writer-tail CAS stays disarmed.
	s.setPersistedBaseline(path, digest, version, revision, true, true, rewriteVersion, nil)
}

// markPersistedFromLoad anchors a loaded ledger. It is not write-verified, so
// it never arms the snapshot no-op. view is the on-disk transcript, or the
// pre-repair raw view when load-time normalization changed it.
func (s *Session) markPersistedFromLoad(path string, digest [sha256.Size]byte, version uint64, revision int64, rewriteVersion int, view []provider.Message) {
	s.setPersistedBaseline(path, digest, version, revision, true, false, rewriteVersion, view)
}

// markPersistedRevisionUnknown records digest+version when the meta sidecar
// is unreadable. Revision CAS stays disarmed until a successful save.
func (s *Session) markPersistedRevisionUnknown(path string, digest [sha256.Size]byte, version uint64, rewriteVersion int, view []provider.Message) {
	s.setPersistedBaseline(path, digest, version, 0, false, false, rewriteVersion, view)
}

func (s *Session) setPersistedBaseline(path string, digest [sha256.Size]byte, version uint64, revision int64, revisionKnown, saveVerified bool, rewriteVersion int, persistedView []provider.Message) {
	s.mu.Lock()
	s.persisted = sessionPersistState{
		path:          canonicalSessionSavePath(path),
		digest:        digest,
		version:       version,
		revision:      revision,
		revisionKnown: revisionKnown,
		saveVerified:  saveVerified,
		ok:            true,
	}
	if rewriteVersion > s.persistedRewriteVersion {
		s.persistedRewriteVersion = rewriteVersion
	}
	if persistedView != nil {
		s.persistedMessages = append([]provider.Message(nil), persistedView...)
		s.persistedViewPath = canonicalSessionSavePath(path)
	} else {
		s.persistedMessages = nil
		s.persistedViewPath = ""
	}
	if saveVerified {
		s.normalizedDirty = false
		s.rawMessages = nil
		s.eventLogDamaged = false
	}
	writer := s.writeWriterLocked()
	logTail := int64(0)
	if info, err := os.Stat(store.SessionEventLog(path)); err == nil {
		logTail = info.Size()
	}
	s.mu.Unlock()
	if writer != nil {
		writer.RecordBaseline(path, revision, digestString(digest), revisionKnown, logTail)
	}
}

// writeWriterLocked returns the SessionWriter behind the bound authority.
// Callers hold s.mu.
func (s *Session) writeWriterLocked() *SessionWriter {
	if s.writeAuth == nil {
		return nil
	}
	return s.writeAuth.writer
}

// syncWriterBaseline copies this session's persist ledger onto the bound
// writer so the first save after Bind can CAS against the event-log tail.
func (s *Session) syncWriterBaseline(path string) {
	if s == nil {
		return
	}
	st := s.persistState(path)
	s.mu.RLock()
	writer := s.writeWriterLocked()
	s.mu.RUnlock()
	if writer == nil || !st.ok || !st.revisionKnown {
		return
	}
	logTail := int64(0)
	if info, err := os.Stat(store.SessionEventLog(path)); err == nil {
		logTail = info.Size()
	}
	writer.RecordBaseline(path, st.revision, digestString(st.digest), true, logTail)
}
