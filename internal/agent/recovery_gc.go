package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Recovery-branch garbage collection. Conflict recovery forks a copy of the
// in-memory transcript whenever a save conflicts (#5993); the triggers are
// fixed, but every fork that ever happened still sits in the session list
// until the user trashes it by hand. Most of them preserve nothing: the
// original session went on to contain everything the fork saved. Those — and
// only those — are safe to reclaim automatically.

// RecoveryGCGracePeriod is how long a reclaimable recovery branch must sit
// idle before GC may collect it. A fresh fork is part of an active conflict
// flow — the user may be comparing it against the original right now.
const RecoveryGCGracePeriod = 24 * time.Hour

// SessionLeaseHeld reports whether ANY live runtime — this process included —
// holds the session's write lease. SessionLeaseHeldByOtherRuntime deliberately
// answers false for the current process; GC needs the stricter question, since
// a branch open in one of our own tabs is just as much in use.
func SessionLeaseHeld(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, ok := sessionLeaseOwners.Load(canonicalSessionSavePath(path)); ok {
		return true
	}
	return SessionLeaseHeldByOtherRuntime(path)
}

// ReclaimableRecoveryBranches scans dir for conflict-recovery branches that
// are safe to dispose of. Every condition must hold — when in doubt the branch
// stays, because a recovery branch exists precisely to prevent data loss:
//
//  1. The branch meta says Recovered and records the fork digest.
//  2. The transcript still matches that fork digest: the branch was never
//     continued on. A single follow-up turn disqualifies it permanently.
//  3. The parent transcript (meta.ParentID, same directory) exists and covers
//     the branch content — equal digest, or the branch is a strict prefix
//     (allowing a compatible leading-system swap). These are the same checks
//     SaveRecoveryBranch uses to declare a recovery not needed in the first
//     place, so "covered" here means the fork preserves nothing unique.
//  4. No live runtime holds the branch's session lease.
//  5. The branch has been idle for at least grace.
//
// It returns candidate paths only; disposal (trash, delete) is caller policy.
func ReclaimableRecoveryBranches(dir string, now time.Time, grace time.Duration) ([]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || strings.HasSuffix(e.Name(), ".events.jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !IsVisibleSession(path) {
			continue
		}
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok || !meta.Recovered || strings.TrimSpace(meta.RecoveryDigest) == "" {
			continue
		}
		if strings.TrimSpace(meta.ParentID) == "" {
			continue
		}
		idleSince := meta.UpdatedAt
		if idleSince.IsZero() {
			info, err := e.Info()
			if err != nil {
				continue
			}
			idleSince = info.ModTime()
		}
		if now.Sub(idleSince) < grace {
			continue
		}
		if SessionLeaseHeld(path) {
			continue
		}
		branch, err := LoadSession(path)
		if err != nil || branch == nil {
			continue
		}
		branchMsgs := branch.Snapshot()
		branchDigest, err := digestSessionMessages(branchMsgs)
		if err != nil || digestString(branchDigest) != meta.RecoveryDigest {
			// Continued on (or undigestable): this is someone's conversation now.
			continue
		}
		parentPath := filepath.Join(dir, meta.ParentID+".jsonl")
		if parentPath == path || !IsVisibleSession(parentPath) {
			continue
		}
		parent, err := LoadSession(parentPath)
		if err != nil || parent == nil {
			continue
		}
		parentMsgs := parent.Snapshot()
		parentDigest, err := digestSessionMessages(parentMsgs)
		if err != nil {
			continue
		}
		covered := bytes.Equal(parentDigest[:], branchDigest[:]) ||
			messagesHavePrefix(parentMsgs, branchMsgs) ||
			messagesHavePrefixWithCompatibleSystem(parentMsgs, branchMsgs)
		if !covered {
			continue
		}
		out = append(out, path)
	}
	return out, nil
}
