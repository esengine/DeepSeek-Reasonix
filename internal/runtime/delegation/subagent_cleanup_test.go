package delegation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/store"
)

func TestDeleteSubagentsByParentSweepsEventLogLeftovers(t *testing.T) {
	sessionDir := testenv.TempDir(t)
	subDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ref := "sa_cleanup_test"
	sessionPath := filepath.Join(subDir, ref+".jsonl")
	meta := sessionstore.SubagentMeta{Ref: ref, ParentSession: "parent-1", Status: sessionstore.SubagentCompleted}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		sessionPath,
		filepath.Join(subDir, ref+".meta.json"),
		// Leftovers from builds where sub-agent saves bootstrapped event logs.
		store.SessionEventLog(sessionPath),
		store.SessionEventIndex(sessionPath),
	}
	for _, p := range files {
		content := []byte("{}\n")
		if filepath.Base(p) == ref+".meta.json" {
			content = metaBytes
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	dirs := store.SessionSidecarDirs(sessionPath)
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "entry"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := DeleteSubagentsByParent(sessionDir, "parent-1"); err != nil {
		t.Fatalf("DeleteSubagentsByParent: %v", err)
	}
	for _, p := range append(files, dirs...) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("subagent artifact survived delete: %s (err=%v)", p, err)
		}
	}
}

func TestSubagentSaveUsesDurableEventLog(t *testing.T) {
	sessionDir := testenv.TempDir(t)
	subDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subDir, "sa_force.jsonl")
	s := sessionstore.NewSession("sub")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(path)); err != nil {
		t.Fatalf("subagent event log missing after save: %v", err)
	}
	if _, err := os.Stat(store.SessionEventIndex(path)); err != nil {
		t.Fatalf("subagent event index missing after save: %v", err)
	}
}
