package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestSaveSnapshotRebuildsZeroByteCheckpointFromEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	original := NewSession("sys")
	original.Add(provider.Message{Role: provider.RoleUser, Content: "before crash"})
	original.Add(provider.Message{Role: provider.RoleAssistant, Content: "saved answer"})
	if err := original.SaveSnapshot(path); err != nil {
		t.Fatalf("initial SaveSnapshot: %v", err)
	}

	logPath := store.SessionEventLog(path)
	if info, err := os.Stat(logPath); err != nil || info.Size() == 0 {
		t.Fatalf("event log = %q, err=%v; want a non-empty WAL", info, err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("truncate checkpoint: %v", err)
	}

	resumed, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after zero-byte checkpoint: %v", err)
	}
	if got := len(resumed.Snapshot()); got != len(original.Snapshot()) {
		t.Fatalf("loaded message count = %d, want %d from WAL", got, len(original.Snapshot()))
	}

	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "after recovery"})
	if err := resumed.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot after WAL recovery: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat healed checkpoint: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("checkpoint remained zero bytes after a valid WAL recovery")
	}
	reloaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession after healed save: %v", err)
	}
	if got := reloaded.Snapshot()[len(reloaded.Snapshot())-1].Content; got != "after recovery" {
		t.Fatalf("reloaded tail = %q, want recovered write", got)
	}
}

func TestSaveSnapshotDoesNotRewriteZeroByteCheckpointFromInvalidEventLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create zero-byte checkpoint: %v", err)
	}
	logPath := store.SessionEventLog(path)
	if err := os.WriteFile(logPath, []byte("not a native event log\n"), 0o600); err != nil {
		t.Fatalf("create invalid event log: %v", err)
	}

	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession with invalid event log: %v", err)
	}
	if len(loaded.Snapshot()) != 0 {
		t.Fatalf("invalid event log produced %d messages, want no fabricated history", len(loaded.Snapshot()))
	}

	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "new explicit turn"})
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot with invalid event log: %v", err)
	}
	checkpoint, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if len(checkpoint) == 0 {
		t.Fatal("explicit new turn did not produce a checkpoint")
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invalid event log: %v", err)
	}
	if string(log) != "not a native event log\n" {
		t.Fatalf("invalid event log was overwritten: %q", log)
	}
	if _, err := LoadSession(path); errors.Is(err, ErrSessionReplayLimitExceeded) {
		t.Fatalf("unexpected replay limit error after checkpoint-only save: %v", err)
	}
}
