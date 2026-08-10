package agent

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestMigrateLegacySessionsRehomesNativeWALAheadOfCheckpoint(t *testing.T) {
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
}
