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

import (
	"os"
	"path/filepath"
	"strings"
)

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

// SessionRecoveryState is the persisted Auto-mode recovery checkpoint state
// (<id>.recovery.json). It is a regular session-owned sidecar, not a transcript.
func SessionRecoveryState(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".recovery.json"
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

// SessionScheduledTasks is the persisted scheduled-task store for /loop
// (scheduled-tasks.json). It lives in the project's .reasonix directory — one
// file per working directory, not per session and not global — so every chat
// launched in the same folder shares that folder's cron system, and crons
// survive /new, /clear, and session deletion. Without a workspace root there
// is no per-directory anchor, so persistence is disabled (""): sessions
// without a root keep tasks in memory only rather than leaking a file that
// session deletion no longer sweeps.
func SessionScheduledTasks(workspaceRoot, sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	if strings.TrimSpace(workspaceRoot) != "" {
		return filepath.Join(workspaceRoot, ".reasonix", "scheduled-tasks.json")
	}
	return ""
}

// LegacyScheduledTasks is the historical per-session sidecar layout
// (<id>.scheduled-tasks.json, beside the transcript) used before the
// per-directory store. It exists only for one-time migration.
func LegacyScheduledTasks(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".scheduled-tasks.json"
}

// MigrateScheduledTasks performs the one-time upgrade from the legacy
// beside-session sidecar to the per-directory store: if the new file does not
// exist yet and a legacy sidecar does, its raw contents are copied to the new
// location and the legacy file is removed. It reports whether an import
// happened. Best-effort: any failure leaves both files untouched and the next
// session retries.
func MigrateScheduledTasks(workspaceRoot, sessionPath string) bool {
	newPath := SessionScheduledTasks(workspaceRoot, sessionPath)
	legacy := LegacyScheduledTasks(sessionPath)
	if newPath == "" || legacy == "" {
		return false
	}
	if _, err := os.Stat(newPath); err == nil {
		return false // already migrated or a fresh store exists
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return false
	}
	// Atomic temp+rename, matching the scheduler's own persistence: a crash
	// mid-write leaves no partial file at newPath, so the next session retries
	// the migration instead of treating a corrupt store as already migrated.
	tmp := newPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false
	}
	if err := os.Rename(tmp, newPath); err != nil {
		_ = os.Remove(tmp)
		return false
	}
	if err := os.Remove(legacy); err != nil {
		return false // the copy is live; the stale legacy file is harmless now
	}
	return true
}

// SessionEventLog is the append-only transcript event log (<id>.events.jsonl).
func SessionEventLog(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".events.jsonl"
}

// SessionEventLogDamaged is the salvage sidecar for event-log bytes that tail
// repair would otherwise discard (<id>.events.jsonl.damaged). It must NOT end
// in .jsonl: older binaries scanning a shared session directory classify any
// non-excluded .jsonl file as a primary transcript and would resurrect the
// damaged bytes as a phantom session.
func SessionEventLogDamaged(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return SessionEventLog(sessionPath) + ".damaged"
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
// The /loop scheduled-task file is deliberately NOT listed: it is owned by the
// working directory (<root>/.reasonix/scheduled-tasks.json), not by the
// session, so crons survive /new, /clear, and session deletion. Every surface
// that deletes a session (desktop trash, /clear, serve, ACP) must remove all
// of these — the event log is the authoritative transcript, so leaving it
// behind both leaks the "deleted" conversation and lets LoadSession resurrect
// it. Directory artifacts (checkpoints, jobs) and ephemeral lock/lease files
// have their own lifecycles and are intentionally not listed.
func SessionSidecarFiles(sessionPath string) []string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}
	return []string{
		SessionMeta(sessionPath),
		SessionGoalState(sessionPath),
		SessionEventLog(sessionPath),
		SessionEventLogDamaged(sessionPath),
		SessionEventIndex(sessionPath),
		SessionConflictLog(sessionPath),
		SessionRecoveryState(sessionPath),
	}
}
