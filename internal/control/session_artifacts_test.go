package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/guardian"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// /clear must not leave the event log behind: it is the authoritative
// transcript, so a leftover both leaks the discarded conversation and lets
// LoadSession resurrect it if the path is ever reused.
func TestRemoveSessionArtifactsSweepsEventLogAndSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "secret work"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot append: %v", err)
	}
	for _, extra := range []string{
		store.SessionGoalState(path),
		store.SessionConflictLog(path),
		guardian.PathFor(path),
		guardian.CursorPathFor(path),
		store.SessionEventLog(guardian.PathFor(path)),
	} {
		if err := os.WriteFile(extra, []byte("{}\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", extra, err)
		}
	}

	if err := removeSessionArtifacts(path); err != nil {
		t.Fatalf("removeSessionArtifacts: %v", err)
	}

	for _, p := range []string{
		path,
		store.SessionMeta(path),
		store.SessionGoalState(path),
		store.SessionEventLog(path),
		store.SessionEventIndex(path),
		store.SessionConflictLog(path),
		guardian.PathFor(path),
		guardian.CursorPathFor(path),
		store.SessionEventLog(guardian.PathFor(path)),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("artifact survived clear: %s (err=%v)", p, err)
		}
	}
	if _, err := agent.LoadSession(path); !os.IsNotExist(err) {
		t.Fatalf("LoadSession after clear = %v, want IsNotExist (no resurrection)", err)
	}
}
