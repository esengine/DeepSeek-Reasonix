package rootenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/runtime/isolation"
	"reasonix/internal/state/sessionstore"
)

func TestConsecutivePublicationsKeepIndependentFrozenDependencies(t *testing.T) {
	s, source := fixture(t)
	write(t, filepath.Join(source, ".gitignore"), "deps/\n")
	if err := os.Mkdir(filepath.Join(source, "deps"), 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(source, "deps", "input"), "dependency\n")
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "root.jsonl"))
	e, err := s.Prepare(t.Context(), path, []string{"deps"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	store := isolation.NewStore(t.TempDir(), source, s.repo).WithMutationCoordinator(s.mutations).WithEnvironmentCopies([]string{"deps"})
	for _, content := range []string{"first\n", "second\n"} {
		write(t, filepath.Join(e.Record().Candidate.WorkspaceRoot, "a"), content)
		p, err := e.BeginPublication(t.Context(), path)
		if err != nil || p == nil {
			t.Fatalf("begin %q: %+v %v", content, p, err)
		}
		if _, err := store.ImportSnapshotResult(t.Context(), p.Import()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Apply(t.Context(), p.ResultID); err != nil {
			t.Fatal(err)
		}
		if err := e.AcknowledgePublication(t.Context(), path, p.ID, store); err != nil {
			t.Fatal(err)
		}
	}
	id := e.Record().ID
	e.Close()
	restored, err := s.Open(t.Context(), id, path)
	if err != nil {
		t.Fatal(err)
	}
	restored.Close()
}
