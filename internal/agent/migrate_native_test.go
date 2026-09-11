package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestMigrateLegacySessionsRehomesNativeWALAheadOfCheckpoint(t *testing.T) {
	useSchemaOneLog(t)
	src := t.TempDir()
	global := t.TempDir()
	workspace := t.TempDir()
	projRoot := t.TempDir()
	router := func(ws string) string { return filepath.Join(projRoot, filepath.Base(ws), "sessions") }
	srcLog := filepath.Join(src, "chat-1.events.jsonl")
	if err := os.WriteFile(srcLog, []byte(legacyEventLog), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLegacyMeta(t, src, "chat-1", workspace, "native flat import")

	flat := filepath.Join(global, "chat-1.jsonl")
	base := NewSession("system prompt")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "checkpoint"})
	if err := base.Save(flat); err != nil {
		t.Fatalf("seed native flat session: %v", err)
	}
	loaded, err := LoadSession(flat)
	if err != nil {
		t.Fatalf("load native flat session: %v", err)
	}
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "WAL tail survives migration"})
	// Normal append saves make the WAL authoritative before the compatibility
	// checkpoint catches up. Migration must copy that WAL-backed view.
	if err := loaded.SaveSnapshot(flat); err != nil {
		t.Fatalf("append native flat session: %v", err)
	}
	pinned := map[string]any{"schemaVersion": 1, "sessionId": "chat-1", "files": []string{"README.md"}}
	pinnedRaw, err := json.Marshal(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionPinnedContext(flat), pinnedRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(srcLog)
	if err != nil {
		t.Fatalf("legacy source log: %v", err)
	}
	if err := os.Chtimes(flat, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("align flat import mtime: %v", err)
	}

	n, err := MigrateLegacySessions(src, global, router)
	if err != nil || n != 1 {
		t.Fatalf("migrate native flat session: n=%d err=%v, want 1 nil", n, err)
	}
	dest := filepath.Join(projRoot, filepath.Base(workspace), "sessions", "chat-1.jsonl")
	migrated, err := LoadSession(dest)
	if err != nil {
		t.Fatalf("load re-homed native session: %v", err)
	}
	if got := migrated.Messages[len(migrated.Messages)-1].Content; got != "WAL tail survives migration" {
		t.Fatalf("re-homed session lost WAL tail: got %q", got)
	}
	if _, err := os.Stat(flat); !os.IsNotExist(err) {
		t.Errorf("native flat checkpoint should be retired: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(flat)); !os.IsNotExist(err) {
		t.Errorf("native flat WAL should be retired: %v", err)
	}
	migratedPinnedRaw, err := os.ReadFile(store.SessionPinnedContext(dest))
	if err != nil {
		t.Fatalf("read migrated pinned sidecar: %v", err)
	}
	var migratedPinned map[string]any
	if err := json.Unmarshal(migratedPinnedRaw, &migratedPinned); err != nil {
		t.Fatal(err)
	}
	if migratedPinned["sessionId"] != "chat-1" {
		t.Fatalf("migrated pinned sessionId = %v", migratedPinned["sessionId"])
	}
}

func TestMigratePinnedContextSidecarRewritesSessionIDAndPreservesFields(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	newPath := filepath.Join(dir, "new.jsonl")
	raw := []byte(`{"schemaVersion":1,"sessionId":"old","files":["README.md"],"future":{"keep":true}}`)
	if err := os.WriteFile(store.SessionPinnedContext(oldPath), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migratePinnedContextSidecar(oldPath, newPath, "new"); err != nil {
		t.Fatal(err)
	}
	migratedRaw, err := os.ReadFile(store.SessionPinnedContext(newPath))
	if err != nil {
		t.Fatal(err)
	}
	var migrated map[string]any
	if err := json.Unmarshal(migratedRaw, &migrated); err != nil {
		t.Fatal(err)
	}
	if migrated["sessionId"] != "new" || migrated["future"] == nil {
		t.Fatalf("migrated sidecar = %v", migrated)
	}
	if _, err := os.Stat(store.SessionPinnedContext(oldPath)); !os.IsNotExist(err) {
		t.Fatalf("old sidecar survived migration: %v", err)
	}
}
