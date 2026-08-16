package agent

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/provider"
)

// ownsWritableBaseline reports whether this session may rewrite path at the
// current disk revision. Ownership requires either a digest match against the
// session's persisted baseline, or a live generation-bound write authority for
// the path at the same revision. Process-level "I hold a lease" alone is not
// enough: a stale controller after rebind must not rewrite under a successor.
func (s *Session) ownsWritableBaseline(path string, existingDigest, rawDigest [sha256.Size]byte, rawDiffers bool, existingRevision int64, existingLedgerDigest string, nextVersion uint64) bool {
	if s.ownsPersistedState(path, existingDigest, existingRevision, existingLedgerDigest, nextVersion) {
		return true
	}
	if rawDiffers && s.ownsPersistedState(path, rawDigest, existingRevision, existingLedgerDigest, nextVersion) {
		return true
	}
	state := s.persistState(path)
	if !state.ok || !state.revisionKnown || state.version > nextVersion {
		return false
	}
	if existingRevision != 0 && state.revision != existingRevision {
		return false
	}
	// Authority-bound same-revision reshape (tool preview, load normalize).
	return s.hasValidWriteAuthority(path)
}

// fixedWriterRecoverySessionPath is the process-wide stable recovery path
// for originalPath. Nested -recovery- names peel back to the root branch.
func fixedWriterRecoverySessionPath(originalPath string) string {
	return stableRecoverySessionPath(originalPath, SessionWriterID())
}

func recoveryRootID(path string) string {
	if root, ok := RecoveryFilenameRootID(path); ok {
		return root
	}
	id := strings.TrimSpace(BranchID(path))
	if id == "" {
		return "session"
	}
	return id
}

// stableRecoverySessionPath is one recovery file per (root branch, generation).
// The 16-hex suffix stays so older desktop discovery still recognizes the file.
func stableRecoverySessionPath(originalPath, generation string) string {
	root := recoveryRootID(originalPath)
	if strings.TrimSpace(generation) == "" {
		generation = SessionWriterID()
	}
	sum := sha256.Sum256([]byte(root + "\x00" + generation))
	suffix := fmt.Sprintf("-recovery-%x", sum[:8])
	stem := recoveryParentStem(root)
	if stem+suffix == BranchID(originalPath) {
		return originalPath
	}
	return filepath.Join(filepath.Dir(originalPath), stem+suffix+".jsonl")
}

func (s *Session) recoveryGenerationKey() string {
	if s == nil {
		return SessionWriterID()
	}
	s.mu.Lock()
	if s.recoveryLane != "" {
		lane := s.recoveryLane
		s.mu.Unlock()
		return lane
	}
	s.mu.Unlock()
	candidate := ""
	if auth := s.WriteAuthority(); auth != nil && auth.Generation() != 0 {
		writerID := SessionWriterID()
		if writer := auth.Writer(); writer != nil && strings.TrimSpace(writer.WriterID()) != "" {
			writerID = writer.WriterID()
		}
		candidate = fmt.Sprintf("%s\x00gen-%d", writerID, auth.Generation())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recoveryLane == "" {
		if candidate == "" {
			candidate = newSessionWriterID()
		}
		s.recoveryLane = candidate
	}
	return s.recoveryLane
}

func (s *Session) isolatedRecoverySessionPath(originalPath string) (string, string) {
	gen := s.recoveryGenerationKey()
	return stableRecoverySessionPath(originalPath, gen), gen
}

func (s *Session) rotateRecoveryLane(current string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recoveryLane == "" || s.recoveryLane == current {
		s.recoveryLane = newSessionWriterID()
	}
}

// writeRecoveryEventLog writes a recovery event log. Isolated writer lanes
// compact so repeated in-place rewrites stay bounded.
func writeRecoveryEventLog(path string, msgs []provider.Message, digest [sha256.Size]byte, isolated bool) error {
	if isolated {
		return compactSessionEventLog(path, msgs, digest, 0, "recovery")
	}
	return appendSessionReplaceEvent(path, msgs, digest, 0, "recovery")
}
