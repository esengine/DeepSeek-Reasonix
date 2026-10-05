package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Redundancy along a recovery lineage: a save that keeps conflicting forks
// root ← A ← B ← C, so a branch nobody continued on is a strict prefix of its
// successor — which parent coverage misses, the parent being the older file.

// recoveryBranchRedundant reports whether a branch preserves nothing of its
// own — its parent went on to contain it, or a later branch of its lineage
// does. Conservative on every unreadable or ambiguous input.
func recoveryBranchRedundant(path, dir string) bool {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
		return false
	}
	if recoveryBranchCoveredByParent(path, dir, meta) {
		return true
	}
	_, ok = recoveryBranchSuccessor(path, dir, meta)
	return ok
}

// recoveryBranchSuccessor finds a recovery branch that still holds everything
// path holds. Lineage links are not required: reclaiming one member of a chain
// severs them, and what authorizes removal is that the turns survive elsewhere,
// not who forked from whom.
func recoveryBranchSuccessor(path, dir string, meta BranchMeta) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = filepath.Dir(path)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".events.jsonl") {
			continue
		}
		candidate := filepath.Join(dir, e.Name())
		if candidate == path {
			continue
		}
		if candidateMeta, ok, err := LoadBranchMeta(candidate); err != nil || !ok || !candidateMeta.Recovered {
			continue
		}
		if recoveryBranchCoveredBySuccessor(path, candidate, meta) {
			return candidate, true
		}
	}
	return "", false
}

// recoveryBranchCoveredBySuccessor proves one pair: path was never continued on
// after its fork, successor is a visible session, and successor's transcript
// contains path's complete history.
func recoveryBranchCoveredBySuccessor(path, successor string, meta BranchMeta) bool {
	if !IsVisibleSession(successor) {
		return false
	}
	branch, err := LoadSession(path)
	if err != nil || branch == nil {
		return false
	}
	msgs := branch.Snapshot()
	digest, err := DigestSessionMessages(msgs)
	if err != nil || digestString(digest) != strings.TrimSpace(meta.RecoveryDigest) {
		// Continued on (or undigestable): this is someone's conversation now.
		return false
	}
	later, err := LoadSession(successor)
	if err != nil || later == nil {
		return false
	}
	laterMsgs := later.Snapshot()
	if !messagesHavePrefix(laterMsgs, msgs) && !messagesHavePrefixWithCompatibleSystem(laterMsgs, msgs) {
		return false
	}
	// Two copies of one transcript cover each other. Keeping the later path
	// stops both from proving the other redundant and vanishing together.
	return len(laterMsgs) > len(msgs) || path < successor
}

// tryAcquireRecoveryCoverageGuard holds whichever session proves path
// redundant — its parent, or the successor that superseded it — so the proof
// cannot be invalidated by a writer while the branch is being removed.
func tryAcquireRecoveryCoverageGuard(path, dir string) (*SessionRemovalGuard, error) {
	guard, err := TryAcquireRecoveryParentGuard(path, dir)
	if err == nil {
		return guard, nil
	}
	if !errors.Is(err, ErrRecoveryBranchNotCovered) {
		return nil, err
	}
	return tryAcquireRecoverySuccessorGuard(path, dir)
}

func tryAcquireRecoverySuccessorGuard(path, dir string) (*SessionRemovalGuard, error) {
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
		return nil, ErrRecoveryBranchNotCovered
	}
	successor, ok := recoveryBranchSuccessor(path, dir, meta)
	if !ok {
		return nil, ErrRecoveryBranchNotCovered
	}
	guard, err := TryAcquireSessionRemovalGuard(successor)
	if err != nil {
		return nil, err
	}
	// Re-prove against the same file now that it cannot change under us.
	if !recoveryBranchCoveredBySuccessor(path, successor, meta) {
		guard.Release()
		return nil, ErrRecoveryBranchNotCovered
	}
	return guard, nil
}

// recoveryRootOf resolves the conversation a new copy belongs to: the parent's
// own root when the parent is itself a copy, otherwise the parent.
func recoveryRootOf(originalPath, parentID string) string {
	if meta, ok, err := LoadBranchMeta(originalPath); err == nil && ok && meta.Recovered {
		if root := strings.TrimSpace(meta.RecoveryRootID); root != "" {
			return root
		}
		if parent := strings.TrimSpace(meta.ParentID); parent != "" {
			return parent
		}
	}
	return parentID
}
