package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/store"
)

func TestPinnedContextSidecarMovesThroughTrashAndRestore(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := savePinnedContextState(sessionPath, []string{"README.md"}); err != nil {
		t.Fatal(err)
	}
	pinnedPath := store.SessionPinnedContext(sessionPath)
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pinnedPath); !os.IsNotExist(err) {
		t.Fatalf("live pinned sidecar survived trash: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "session.jsonl", "session.jsonl")
	trashPinned := filepath.Join(filepath.Dir(trashPath), "session.pinned-context.json")
	if _, err := os.Stat(trashPinned); err != nil {
		t.Fatalf("pinned sidecar missing from trash: %v", err)
	}
	if err := restoreTrashedSessionFile(dir, trashPath); err != nil {
		t.Fatal(err)
	}
	state, err := loadPinnedContextState(sessionPath)
	if err != nil || len(state.Files) != 1 || state.Files[0] != "README.md" {
		t.Fatalf("restored pinned state = %+v, err=%v", state, err)
	}
	if _, err := os.Stat(trashPinned); !os.IsNotExist(err) {
		t.Fatalf("trash pinned sidecar survived restore: %v", err)
	}
}

func TestRemoveDesktopSessionArtifactsRemovesPinnedContext(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := savePinnedContextState(sessionPath, []string{"README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := removeDesktopSessionArtifacts(sessionPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.SessionPinnedContext(sessionPath)); !os.IsNotExist(err) {
		t.Fatalf("pinned sidecar survived permanent removal: %v", err)
	}
}
