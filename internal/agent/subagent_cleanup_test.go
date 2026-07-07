package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/store"
)

func TestDeleteSubagentsByParentSweepsEventLogLeftovers(t *testing.T) {
	sessionDir := t.TempDir()
	subDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ref := "sa_cleanup_test"
	sessionPath := filepath.Join(subDir, ref+".jsonl")
	meta := SubagentMeta{Ref: ref, ParentSession: "parent-1", Status: SubagentCompleted}
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

	if err := DeleteSubagentsByParent(sessionDir, "parent-1"); err != nil {
		t.Fatalf("DeleteSubagentsByParent: %v", err)
	}
	for _, p := range files {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("subagent artifact survived delete: %s (err=%v)", p, err)
		}
	}
}

func TestSubagentForceSaveStaysSingleFile(t *testing.T) {
	sessionDir := t.TempDir()
	subDir := filepath.Join(sessionDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(subDir, "sa_force.jsonl")
	s := NewSession("sub")
	if err := s.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := os.Stat(store.SessionEventLog(path)); !os.IsNotExist(err) {
		t.Fatalf("repeated force saves created an event log (err=%v)", err)
	}
}
