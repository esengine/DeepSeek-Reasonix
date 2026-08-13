package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

func TestTopicMigrationMarkerDetectsSameSizeRewriteWithRestoredMtime(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := writeLegacySession(t, dir, "same-size.jsonl", "alpha", time.Now().Add(-time.Hour))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat original session: %v", err)
	}
	markTopicMigrationDone(dir)
	if !topicMigrationDone(dir) {
		t.Fatal("fresh migration marker should match")
	}

	if err := os.WriteFile(path, []byte("{\"role\":\"user\",\"content\":\"bravo\"}\n"), 0o644); err != nil {
		t.Fatalf("rewrite same-size session: %v", err)
	}
	if rewritten, err := os.Stat(path); err != nil {
		t.Fatalf("stat rewritten session: %v", err)
	} else if rewritten.Size() != info.Size() {
		t.Fatalf("fixture size changed: before=%d after=%d", info.Size(), rewritten.Size())
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore session mtime: %v", err)
	}
	if topicMigrationDone(dir) {
		t.Fatal("same-size rewrite with restored mtime must invalidate marker")
	}
}

func TestForcedTopicMigrationBypassesMatchingMarker(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := writeLegacySession(t, dir, "forced.jsonl", "force legacy migration", time.Now().Add(-time.Hour))
	markTopicMigrationDone(dir)
	markTopicIndexRepairDone(dir)
	if migrated := migrateLegacySessionsIntoGlobalTopics(dir); len(migrated) != 0 {
		t.Fatalf("ordinary migration ignored matching marker: %v", migrated)
	}
	if _, ok, err := agent.LoadBranchMeta(path); err != nil {
		t.Fatalf("load legacy meta before forced migration: %v", err)
	} else if ok {
		t.Fatal("matching marker should have kept ordinary migration from creating meta")
	}

	migrated, migratedPaths := forceMigrateLegacySessionsIntoGlobalTopicsWithPaths(dir)
	wantTopicID := legacySessionTopicID(path)
	if len(migrated) != 1 || migrated[0] != wantTopicID {
		t.Fatalf("forced migration = %v, want %q", migrated, wantTopicID)
	}
	if len(migratedPaths) != 1 || !sameDesktopPath(migratedPaths[0], path) {
		t.Fatalf("forced migration paths = %v, want %q", migratedPaths, path)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load forced migration meta: ok=%v err=%v", ok, err)
	}
	if strings.TrimSpace(meta.TopicID) != wantTopicID {
		t.Fatalf("forced migration topic = %q, want %q", meta.TopicID, wantTopicID)
	}
}

func TestTopicMigrationMarkerIncludesAuthoritativeEventLog(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := writeLegacySession(t, dir, "event-aware.jsonl", "checkpoint", time.Now().Add(-time.Hour))
	markTopicMigrationDone(dir)
	if !topicMigrationDone(dir) {
		t.Fatal("fresh migration marker should match")
	}
	eventPath := strings.TrimSuffix(path, ".jsonl") + ".events.jsonl"
	if err := os.WriteFile(eventPath, []byte("{\"type\":\"session.header\",\"schemaVersion\":1}\n"), 0o644); err != nil {
		t.Fatalf("write authoritative event log: %v", err)
	}
	if topicMigrationDone(dir) {
		t.Fatalf("new event log %q must invalidate marker", filepath.Base(eventPath))
	}
}
