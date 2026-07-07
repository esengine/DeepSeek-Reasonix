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

// IsSessionTranscriptName reports whether name is a primary session transcript
// file. Append-only event logs and guardian sidecars also end in .jsonl, so
// callers that discover sessions by directory scan must use this helper instead
// of filepath.Ext.
func IsSessionTranscriptName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasSuffix(name, ".jsonl") &&
		!strings.HasSuffix(name, ".events.jsonl") &&
		!strings.HasSuffix(name, ".conflicts.jsonl") &&
		!strings.HasSuffix(name, ".guardian.jsonl")
}

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

// SessionEventLog is the append-only transcript event log (<id>.events.jsonl).
func SessionEventLog(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".events.jsonl"
}

// SessionEventIndex is the listing/checkpoint index for the event log
// (<id>.event-index.json). It contains derived offsets and digests, not the
// transcript body.
func SessionEventIndex(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".event-index.json"
}

// SessionConflictLog is the append-only diagnostic log for snapshot conflict
// recoveries (<id>.conflicts.jsonl). It contains revision counters and branch
// ids, not transcript content.
func SessionConflictLog(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".conflicts.jsonl"
}

// SessionLockFile is the advisory save lock (<id>.jsonl.lock).
func SessionLockFile(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".lock"
}

// SessionLeaseLock is the runtime ownership lock (<id>.jsonl.lease.lock).
func SessionLeaseLock(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".lease.lock"
}

// SessionLeaseInfo is the runtime ownership metadata
// (<id>.jsonl.lease.json).
func SessionLeaseInfo(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".lease.json"
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

// SessionSidecarFiles returns every regular-file sidecar owned by a session
// transcript: branch meta, goal state, event/index logs, and diagnostic logs.
// Every surface that deletes a session (desktop trash, /clear, serve, ACP)
// must remove all of these — the event log is the authoritative transcript, so
// leaving it behind both leaks the "deleted" conversation and lets LoadSession
// resurrect it. Directory artifacts (checkpoints, jobs) and ephemeral
// lock/lease files have their own lifecycles and are intentionally not listed.
func SessionSidecarFiles(sessionPath string) []string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}
	return []string{
		SessionMeta(sessionPath),
		SessionGoalState(sessionPath),
		SessionEventLog(sessionPath),
		SessionEventIndex(sessionPath),
		SessionConflictLog(sessionPath),
	}
}
