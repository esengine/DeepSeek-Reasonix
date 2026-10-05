package acp

import (
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func TestDeleteSessionFilesSweepsEventLogAndSidecars(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "to delete"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	if err := os.WriteFile(acpMetaPath(path), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write acp meta: %v", err)
	}
	if err := os.WriteFile(store.SessionConflictLog(path), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write conflict log: %v", err)
	}

	if err := deleteSessionFiles(path); err != nil {
		t.Fatalf("deleteSessionFiles: %v", err)
	}
	for _, p := range []string{
		path,
		acpMetaPath(path),
		store.SessionMeta(path),
		store.SessionEventLog(path),
		store.SessionEventIndex(path),
		store.SessionConflictLog(path),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("artifact survived delete: %s (err=%v)", p, err)
		}
	}
	if _, err := sessionstore.LoadSession(path); !os.IsNotExist(err) {
		t.Fatalf("LoadSession after delete = %v, want IsNotExist (no resurrection)", err)
	}
}
