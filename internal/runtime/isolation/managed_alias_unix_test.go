//go:build !windows

package isolation

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestManagedCandidateCanonicalAliasesKeepTheSameID(t *testing.T) {
	root, managed, aliases := gitRepo(t), t.TempDir(), t.TempDir()
	sourceAlias, managedAlias := filepath.Join(aliases, "source"), filepath.Join(aliases, "managed")
	if err := os.Symlink(root, sourceAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managed, managedAlias); err != nil {
		t.Fatal(err)
	}
	s := NewStore(managedAlias, sourceAlias, opened(t, sourceAlias))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	write(t, run.WorkspaceRoot(), "a.txt", "alias result\n")
	if err := run.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(managed, root, opened(t, root))
	if !slices.Contains(peer.Pending(), run.ID()) {
		t.Fatal("alias lost original ID")
	}
	e, err := peer.Apply(t.Context(), run.ID())
	if err != nil || e.ID != run.ID() {
		t.Fatalf("alias recovery: %v %v", e, err)
	}
}

func TestManagedCandidateLinkedSlotCannotRedirectCleanup(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slot := filepath.Dir(run.WorkspaceRoot())
	moved := slot + "-moved"
	if err := os.Rename(slot, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, slot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Discard(t.Context(), run.ID()); err == nil {
		t.Fatal("linked slot admitted")
	}
	if _, err := os.Stat(filepath.Join(moved, filepath.Base(run.WorkspaceRoot()))); err != nil {
		t.Fatal("redirected cleanup removed candidate", err)
	}
}
