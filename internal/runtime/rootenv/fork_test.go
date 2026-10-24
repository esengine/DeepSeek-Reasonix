package rootenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/state/sessionstore"
)

func TestForkCapturesParentSceneWithoutChangingSourceBaseline(t *testing.T) {
	s, source := fixture(t)
	parent := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "parent.jsonl"))
	child := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "child.jsonl"))
	e, err := s.Prepare(t.Context(), parent, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), parent, attachmentAuthority(t, parent)); err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	write(t, filepath.Join(r.Candidate.WorkspaceRoot, "a"), "parent change\n")
	fork, err := e.Fork(t.Context(), parent, child, attachmentAuthority(t, child))
	if err != nil {
		t.Fatal(err)
	}
	defer fork.Close()
	f := fork.Record()
	if f.Candidate.WorktreeRoot == r.Candidate.WorktreeRoot || f.Baseline != r.Cursor || f.Source != r.Source || f.ParentID != r.ID {
		t.Fatal("fork changed publication baseline or reused parent", f)
	}
	if f.Materialization == nil || f.Materialization.Commit == f.Baseline.Commit {
		t.Fatal("fork lost dirty scene snapshot")
	}
	if raw, err := os.ReadFile(filepath.Join(f.Candidate.WorkspaceRoot, "a")); err != nil || string(raw) != "parent change\n" {
		t.Fatalf("fork scene: %q %v", raw, err)
	}
	write(t, filepath.Join(f.Candidate.WorkspaceRoot, "a"), "independent change\n")
	if raw, err := os.ReadFile(filepath.Join(r.Candidate.WorkspaceRoot, "a")); err != nil || string(raw) != "parent change\n" {
		t.Fatalf("fork changed parent: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(filepath.Join(source, "a")); err != nil || string(raw) != "base\n" {
		t.Fatalf("fork changed source: %q %v", raw, err)
	}
	if err := fork.CheckSession(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if err := e.CheckSession(t.Context(), child); err == nil {
		t.Fatal("parent accepted child's independent attachment")
	}
	if err := e.Validate(t.Context()); err != nil {
		t.Fatal("fork retired parent owner", err)
	}
}

func TestManagedRootCannotBeNestedInSourceSnapshot(t *testing.T) {
	s, source := fixture(t)
	if _, err := New(filepath.Join(source, "state"), source, s.repo, s.mutations); err == nil {
		t.Fatal("recursive managed snapshot accepted")
	}
}
