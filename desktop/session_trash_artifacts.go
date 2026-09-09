package main

import (
	"strings"

	"reasonix/internal/store"
)

// sessionTrashArtifacts lists every file and directory a session owns, with
// the name each keeps inside the trash entry, so moving a session aside and
// restoring it round-trip the whole set.
func sessionTrashArtifacts(sessionPath, key string) []sessionTrashArtifact {
	stem := strings.TrimSuffix(key, ".jsonl")
	return []sessionTrashArtifact{
		{src: sessionPath, name: key},
		{src: store.SessionMeta(sessionPath), name: key + ".meta"},
		{src: store.SessionGoalState(sessionPath), name: stem + ".goal-state.json"},
		{src: store.SessionEventLog(sessionPath), name: stem + ".events.jsonl"},
		{src: store.SessionEventLogDamaged(sessionPath), name: stem + ".events.jsonl.damaged"},
		{src: store.SessionEventLogRotating(sessionPath), name: stem + ".events.jsonl.rotating"},
		{src: store.SessionEventIndex(sessionPath), name: stem + ".event-index.json"},
		{src: store.SessionDisplayIndex(sessionPath), name: stem + ".display-index.json"},
		{src: store.SessionConflictLog(sessionPath), name: stem + ".conflicts.jsonl"},
		{src: store.SessionRecoveryState(sessionPath), name: stem + ".recovery.json"},
		{src: store.SessionPinnedContext(sessionPath), name: stem + ".pinned-context.json"},
		{src: sessionTelemetryPath(sessionPath), name: key + ".telemetry.json"},
		{src: store.SessionCheckpointDir(sessionPath), name: stem + ".ckpt"},
		{src: store.SessionJobsDir(sessionPath), name: stem + ".jobs"},
		{src: store.SessionInboxDir(sessionPath), name: stem + ".inbox"},
	}
}
