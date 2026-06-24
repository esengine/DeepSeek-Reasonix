// Package store is the single authority for reasonix's on-disk persistence
// layout. Nothing else should construct a persistence path by hand.
//
// This first slice owns the session-artifact sidecars — the files and
// directories that live beside a session's .jsonl (branch metadata, goal state,
// checkpoints, background-job artifacts, the cleanup-pending marker). They were
// previously derived independently in internal/agent, internal/jobs,
// internal/control and internal/acp, each re-spelling the suffix convention; a
// layout change meant hunting across packages. Centralizing them here makes
// store the one place that knows where a session's artifacts go.
//
// store is a leaf: it imports only the standard library, so any package may
// depend on it without risking an import cycle. Root/directory resolution (the
// ~/.reasonix tree) and the desktop root unification land in later slices.
package store

import "strings"

// sessionStem strips the .jsonl suffix so a sidecar sits beside the session as
// <id>.<kind> rather than <id>.jsonl.<kind>.
func sessionStem(sessionPath string) string {
	return strings.TrimSuffix(sessionPath, ".jsonl")
}

// SessionMeta is the branch-metadata sidecar. Unlike the other sidecars it
// appends to the full session path (historical layout), so session.jsonl yields
// session.jsonl.meta.
func SessionMeta(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".meta"
}

// SessionGoalState is the persisted active-goal sidecar (<id>.goal-state.json).
func SessionGoalState(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".goal-state.json"
}

// SessionCheckpointDir is the snapshot-checkpoint directory (<id>.ckpt).
func SessionCheckpointDir(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".ckpt"
}

// SessionJobsDir is the background-job artifact directory (<id>.jobs).
func SessionJobsDir(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".jobs"
}

// SessionCleanupPending is the delayed-cleanup marker (<id>.cleanup-pending.json).
func SessionCleanupPending(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".cleanup-pending.json"
}
