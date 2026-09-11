package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// Matrix pins that must also run on a real Windows runner (session lock,
// delete-disposition, atomic replace). Locally this covers the portable
// generation and lock contracts.
func TestSessionWriterGenerationInvalidatesPriorSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	w, err := AcquireSessionWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Release()
	if err := w.Bind(s, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := w.Bind(s, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	old := s.WriteAuthority()
	if err := w.Bind(s, NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	s.BindWriteAuthority(old)
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "stale"})
	if err := s.SaveSnapshot(path); err == nil {
		t.Fatal("stale generation save succeeded")
	}
}

func TestRepeatedWriterSavesDoNotCreateRecoveryBranches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bindSessionWriter(t, s, path)
	for i := range 8 {
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: "x"})
		if err := s.SaveSnapshot(path); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if got := recoveryJSONL(filepath.Dir(path)); len(got) != 0 {
		t.Fatalf("autosave created recovery files: %v", got)
	}
}
