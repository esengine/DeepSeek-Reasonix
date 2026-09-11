package main

import (
	"encoding/json"
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

func TestRepairIndexedTopicDecodesStaleListingProjection(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := writeLegacySession(t, dir, "stale-projection.jsonl", "authoritative first prompt", time.Now())
	preview, _ := agent.SessionPreview(sessionPath)
	wantTitle := topicTitleFromText(preview)
	topicID := "legacy_stale_projection"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID: agent.BranchID(sessionPath), Scope: "global", TopicID: topicID,
		Revision: 7, ContentDigest: "pre-upgrade-digest", SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	markTopicMigrationDone(dir)
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 1 || repaired[0] != topicID {
		t.Fatalf("repaired topics = %v, want %q", repaired, topicID)
	}
	if got := loadTopicTitle("", topicID); got != wantTitle {
		t.Fatalf("repaired title = %q, want transcript preview title %q", got, wantTitle)
	}
}

func TestRepairIndexedTopicDefersUnreadableTranscriptWithoutPersistingFallback(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := writeLegacySession(t, dir, "temporarily-unreadable.jsonl", "original prompt", time.Now())
	topicID := "legacy_temporarily_unreadable"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID: agent.BranchID(sessionPath), Scope: "global", TopicID: topicID,
		Revision: 7, ContentDigest: "pre-upgrade-digest", SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, []byte("{not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	markTopicMigrationDone(dir)

	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 0 {
		t.Fatalf("unreadable transcript repaired topics = %v, want none", repaired)
	}
	if topicIndexRepairDone(dir) {
		t.Fatal("unreadable transcript must leave the repair marker open for retry")
	}
	if got := loadTopicTitle("", topicID); got != "" {
		t.Fatalf("unreadable transcript persisted fallback title %q", got)
	}
	if got := loadTopicTitleSource("", topicID); got != "" {
		t.Fatalf("unreadable transcript persisted fallback title source %q", got)
	}
	if containsDesktopString(loadProjectsFile().GlobalTopics, topicID) {
		t.Fatal("unreadable transcript was added to the topic index")
	}

	const recoveredPrompt = "recovered authoritative prompt"
	if err := os.WriteFile(sessionPath, []byte("{\"role\":\"user\",\"content\":\""+recoveredPrompt+"\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 1 || repaired[0] != topicID {
		t.Fatalf("retry repaired topics = %v, want %q", repaired, topicID)
	}
	if got, want := loadTopicTitle("", topicID), topicTitleFromText(recoveredPrompt); got != want {
		t.Fatalf("retry title = %q, want %q", got, want)
	}
	if !topicIndexRepairDone(dir) {
		t.Fatal("successful retry should complete the repair marker")
	}
}

func TestRepairIndexedTopicDefersUnreadableTitleSidecar(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := writeLegacySession(t, dir, "custom-title.jsonl", "transcript fallback", time.Now())
	topicID := "legacy_custom_title"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID: agent.BranchID(sessionPath), Scope: "global", TopicID: topicID,
		Revision: 7, ContentDigest: "pre-upgrade-digest", SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionTitlesPath(dir), []byte("{not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	markTopicMigrationDone(dir)

	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 0 {
		t.Fatalf("unreadable title sidecar repaired topics = %v, want none", repaired)
	}
	if topicIndexRepairDone(dir) || loadTopicTitle("", topicID) != "" || loadTopicTitleSource("", topicID) != "" || containsDesktopString(loadProjectsFile().GlobalTopics, topicID) {
		t.Fatal("unreadable title sidecar finalized indexed-topic repair")
	}

	const customTitle = "Recovered custom title"
	titleJSON, err := json.Marshal(map[string]string{filepath.Base(sessionPath): customTitle})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionTitlesPath(dir), titleJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 1 || repaired[0] != topicID {
		t.Fatalf("restored title sidecar repaired topics = %v, want %q", repaired, topicID)
	}
	if got, want := loadTopicTitle("", topicID), topicTitleFromText(customTitle); got != want {
		t.Fatalf("restored custom title = %q, want %q", got, want)
	}
}
