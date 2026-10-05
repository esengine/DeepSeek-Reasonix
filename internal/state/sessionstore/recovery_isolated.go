package sessionstore

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
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

// fixedWriterRecoverySessionPath is one fixed isolated path per writer identity
// so depth-cap / shutdown isolation rewrites in place instead of forking a new
// digest-keyed file on every conflict tick.
func fixedWriterRecoverySessionPath(originalPath string) string {
	return recoverySessionPathForLane(originalPath, SessionWriterID())
}

// siblingRecoveryBranchWithDigest finds a recovery branch of the same parent
// already holding exactly this content. One writer keeps one lane, but another
// writer — or a lane rotated off a transcript it could not keep — can hold the
// same unsaved turns, and identical rows are worth one file. Digests come from
// branch metadata, so this reads sidecars, not transcripts.
func siblingRecoveryBranchWithDigest(originalPath, digestText string) (string, bool) {
	if strings.TrimSpace(digestText) == "" {
		return "", false
	}
	dir := filepath.Dir(originalPath)
	stem := recoveryParentStem(BranchID(originalPath))
	matches, err := filepath.Glob(filepath.Join(dir, stem+"-recovery-*.jsonl"))
	if err != nil {
		return "", false
	}
	slices.Sort(matches)
	for _, path := range matches {
		if path == originalPath || IsCleanupPending(path) {
			continue
		}
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok || !meta.Recovered {
			continue
		}
		if meta.RecoveryDigest == digestText {
			return path, true
		}
	}
	return "", false
}

func recoverySessionPathForLane(originalPath, lane string) string {
	writerDigest := sha256.Sum256([]byte(lane))
	// Keep the established 16-hex suffix so metadata-less recovery files are
	// still recognized by older desktop fallback discovery.
	suffix := fmt.Sprintf("-recovery-%x", writerDigest[:8])
	id := BranchID(originalPath)
	if strings.HasSuffix(id, suffix) {
		return originalPath
	}
	return filepath.Join(filepath.Dir(originalPath),
		fmt.Sprintf("%s%s.jsonl", recoveryParentStem(id), suffix))
}

// writeRecoveryBranchIsolated lands one conflict, preferring a sibling that
// already holds this exact transcript over a new file. A path that turns out to
// hold something else is never retried: branch metadata records the digest at
// fork time, and a branch written to since then no longer matches it.
func (s *Session) writeRecoveryBranchIsolated(
	originalPath string,
	opts RecoveryBranchOptions,
	msgs []provider.Message,
	digest [sha256.Size]byte,
	version uint64,
	rewriteVersion int,
	preview string,
	turns int,
	digestText string,
	recoveryDepth int,
	shutdown bool,
) (RecoveryBranchInfo, error) {
	taken := map[string]bool{}
	for range 8 {
		path, lane := s.isolatedRecoverySessionPath(originalPath, digestText, taken)
		info, collision, err := s.writeRecoveryBranchAtPath(path, opts, msgs, digest,
			version, rewriteVersion, preview, turns, digestText, recoveryDepth, shutdown)
		if err != nil {
			return RecoveryBranchInfo{}, err
		}
		if !collision {
			return info, nil
		}
		taken[path] = true
		s.rotateRecoveryLane(lane)
	}
	return RecoveryBranchInfo{}, fmt.Errorf("allocate isolated recovery lane: too many existing collisions")
}

// isolatedRecoverySessionPath picks where this conflict lands: a sibling that
// already holds the identical transcript, otherwise this writer's lane. An
// empty lane means the writer's own, which a resume, a rebuild, or a reopened
// pane keeps — keying it to the live Session gave each of them a file. The
// lane is returned either way, so a collision rotates it.
func (s *Session) isolatedRecoverySessionPath(originalPath, digestText string, taken map[string]bool) (string, string) {
	s.mu.Lock()
	lane := s.recoveryLane
	s.mu.Unlock()
	if sibling, ok := siblingRecoveryBranchWithDigest(originalPath, digestText); ok && !taken[sibling] {
		return sibling, lane
	}
	if lane == "" {
		return fixedWriterRecoverySessionPath(originalPath), lane
	}
	return recoverySessionPathForLane(originalPath, lane), lane
}

func (s *Session) rotateRecoveryLane(current string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recoveryLane == current {
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
