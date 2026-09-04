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
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// jsonlSidecarSuffixes are the sidecars that end in .jsonl and therefore cannot
// be told apart from a transcript by extension. A session listing classifies by
// name, so one missing from here is shown to the user as a conversation of its
// own. It is one list rather than a chain of conditions because the failure is
// silent: nothing tells the author of the next sidecar that a scan now sees it.
var jsonlSidecarSuffixes = []string{
	".events.jsonl",
	".conflicts.jsonl",
	".guardian.jsonl",
	".wire.jsonl",
	".adjudication.jsonl",
	".execution.jsonl",
}

// IsSessionTranscriptName reports whether name is a primary session transcript
// file. Append-only event logs and guardian sidecars also end in .jsonl, so
// callers that discover sessions by directory scan must use this helper instead
// of filepath.Ext.
func IsSessionTranscriptName(name string) bool {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	for _, suffix := range jsonlSidecarSuffixes {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

// IsSubagentTranscriptName reports a flat legacy subagent transcript, which the
// current layout keeps under a subagents/ tree instead. They are only meaningful
// through the parent session that spawned them, so a listing of the user's own
// conversations excludes them — while on-disk work (redaction, GC, migration)
// still has to see them.
func IsSubagentTranscriptName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(filepath.Base(name)), "subagent-")
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

// SessionContext is the context-projection / compaction-state sidecar
// (<id>.context.json). It holds the model-visible projection and cache
// telemetry; transcript authority remains with the native event log once one
// exists, with the primary .jsonl retained as its compatibility checkpoint.
func SessionContext(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".context.json"
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

// SessionExecution is the append-only record of delegated executions a turn
// opened (<id>.execution.jsonl): that they existed and under what authority,
// written before the work starts because a turn is only appended when it ends.
func SessionExecution(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".execution.jsonl"
}

// SessionAdjudication is the append-only record of host adjudication barriers
// a turn entered (<id>.adjudication.jsonl): what the host asked a person and
// how it ended, kept apart from the transcript because the model never said it.
func SessionAdjudication(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".adjudication.jsonl"
}

// SessionEventLog is the append-only transcript event log (<id>.events.jsonl).
func SessionEventLog(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".events.jsonl"
}

// SessionWireLog is the frontend event-frame log (<id>.wire.jsonl) that lets a
// reopened session rebuild its trajectory pane. Like the other .jsonl sidecars
// it must stay excluded from IsSessionTranscriptName, or a directory scan
// resurrects it as a phantom session.
func SessionWireLog(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".wire.jsonl"
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

// SessionDisplayIndex is the paging sidecar for the transcript
// (<id>.display-index.json). It contains per-message byte offsets, roles, and
// turn boundaries derived from the transcript, never message bodies, so a
// reader can page a huge history without parsing whole session files.
func SessionDisplayIndex(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".display-index.json"
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

// SessionSuperseded holds what a leaseholder's overriding write replaced. The
// suffix deliberately does not end in ".jsonl": a session listing globs for
// those, and the point of keeping these bytes this way is that no second
// conversation shows up next to the one the user is in.
func SessionSuperseded(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".superseded.jsonl.bak"
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

// SessionOutputsDir holds tool output too large to sit in the model's context
// (<id>.outputs/). One file per tool call; the context keeps a pointer and the
// model reads what it needs with read_file.
func SessionOutputsDir(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".outputs"
}

// RemoveSessionArtifacts deletes a transcript and everything beside it. Each
// front end hand-rolled this loop and each dropped something different: a path
// list handed to os.Remove silently skips whatever is not a file, which is how
// spilled output survived every delete. extra takes the per-front-end
// companions (ACP metadata, guardian cursors). Refusals are joined, not hidden.
func RemoveSessionArtifacts(sessionPath string, extra ...string) error {
	paths := append([]string{sessionPath}, extra...)
	paths = append(paths, SessionSidecarFiles(sessionPath)...)
	var errs []error
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	for _, d := range SessionSidecarDirs(sessionPath) {
		if err := os.RemoveAll(d); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SessionPathForOutputsDir inverts SessionOutputsDir: the transcript a spill
// directory belongs to, or empty when the path is not one. A sweep needs the
// mapping backwards, and only this file should know its shape.
func SessionPathForOutputsDir(outputsDir string) string {
	stem, ok := strings.CutSuffix(strings.TrimSpace(outputsDir), ".outputs")
	if !ok || stem == "" {
		return ""
	}
	return stem + ".jsonl"
}

// SessionJobsDir is the background-job artifact directory (<id>.jobs).
func SessionJobsDir(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".jobs"
}

// SessionInboxDir is the durable session-level instruction inbox
// (<id>.inbox/). Manifest metadata and frozen prompt blobs live here.
func SessionInboxDir(sessionPath string) string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return ""
	}
	return sessionStem(sessionPath) + ".inbox"
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
		SessionAdjudication(sessionPath),
		SessionExecution(sessionPath),
		SessionEventLog(sessionPath),
		SessionWireLog(sessionPath),
		SessionEventLogDamaged(sessionPath),
		SessionEventIndex(sessionPath),
		SessionDisplayIndex(sessionPath),
		SessionConflictLog(sessionPath),
		SessionRecoveryState(sessionPath),
		SessionContext(sessionPath),
	}
}

// SessionSidecarDirs returns directory sidecars with no life past the
// transcript. Listed apart because they need RemoveAll — handed to os.Remove
// they fail on their own contents and get skipped, which is how spilled output
// outlived the conversations it belonged to. Checkpoints, jobs and the inbox
// keep their own lifecycles and stay out.
func SessionSidecarDirs(sessionPath string) []string {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return nil
	}
	return []string{SessionOutputsDir(sessionPath)}
}
