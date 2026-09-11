package agent

import (
	"crypto/sha256"
	"os"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// classifySnapshotWrite decides the write shape for a save. Writer-bound
// sessions try the event-log-tail CAS first and only fall back to a full
// disk classification when that tail no longer matches this writer's baseline.
func (s *Session) classifySnapshotWrite(path string, next []provider.Message, nextDigest [sha256.Size]byte, nextVersion uint64, allowOwnedRewrite bool) (snapshotWriteDecision, error) {
	if decision, ok := s.writerTailDecision(path, next, nextDigest, allowOwnedRewrite); ok {
		return decision, nil
	}
	return s.checkSnapshotWrite(path, next, nextDigest, nextVersion, allowOwnedRewrite)
}

// writerTailDecision is the event-log-tail CAS for writer-bound saves.
// ok is false unless a live SessionWriter authority, a paired persist view,
// and an unchanged event-log tail (size + index revision/digest) all agree.
func (s *Session) writerTailDecision(path string, next []provider.Message, nextDigest [sha256.Size]byte, allowOwnedRewrite bool) (snapshotWriteDecision, bool) {
	s.mu.RLock()
	auth := s.writeAuth
	normalizedDirty := s.normalizedDirty
	damaged := s.eventLogDamaged
	persisted := s.persistedMessages
	viewPath := s.persistedViewPath
	s.mu.RUnlock()
	if auth == nil || auth.writer == nil {
		return snapshotWriteDecision{}, false
	}
	if normalizedDirty || damaged || !auth.Valid() {
		return snapshotWriteDecision{}, false
	}
	key := canonicalSessionSavePath(path)
	if viewPath != key {
		return snapshotWriteDecision{}, false
	}
	base, ok := auth.writer.Baseline(path)
	if !ok || !base.RevisionKnown || base.LogTail == 0 {
		return snapshotWriteDecision{}, false
	}
	idx, err := readSessionEventIndex(path)
	if err != nil || idx == nil ||
		idx.LogSize != base.LogTail || idx.Revision != base.Revision || idx.ContentDigest != base.ContentDigest {
		return snapshotWriteDecision{}, false
	}
	info, err := os.Stat(store.SessionEventLog(path))
	if err != nil || info.Size() != base.LogTail {
		return snapshotWriteDecision{}, false
	}
	ledgerRevision, ledgerDigest, err := sessionContentRevision(path)
	if err != nil || ledgerRevision != base.Revision || ledgerDigest != base.ContentDigest {
		// A failed commit can reserve the next metadata revision before its WAL
		// record lands. Treat that reservation as a changed tail identity so the
		// full disk classifier adopts it as the next commit's base.
		return snapshotWriteDecision{}, false
	}

	nextDigestHex := digestString(nextDigest)
	if base.ContentDigest == nextDigestHex {
		return snapshotWriteDecision{revision: base.Revision, upToDate: true}, true
	}
	if allowOwnedRewrite {
		return snapshotWriteDecision{revision: base.Revision}, true
	}
	if messagesHavePrefix(next, persisted) {
		if len(persisted) < len(next) {
			return snapshotWriteDecision{revision: base.Revision, appendOnly: true, appendFrom: len(persisted)}, true
		}
		return snapshotWriteDecision{revision: base.Revision}, true
	}
	if messagesHavePrefixWithCompatibleSystem(next, persisted) {
		return snapshotWriteDecision{revision: base.Revision}, true
	}
	return snapshotWriteDecision{}, false
}
