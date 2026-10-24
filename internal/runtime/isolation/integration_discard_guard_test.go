package isolation

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/worktree"
)

func TestInterruptedIntegrationDiscardCannotRemoveRecreatedCandidate(t *testing.T) {
	store, original := integrationFixture(t)
	i, err := store.PrepareIntegration(t.Context(), original.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	id, root := i.ID(), i.WorkspaceRoot()
	status := i.Status()
	record, err := store.integrationPath(id)
	if err != nil {
		t.Fatal(err)
	}
	previous := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = previous }()
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == record {
			if _, err := os.Lstat(root); os.IsNotExist(err) {
				panic("interrupted discard receipt")
			}
		}
	}
	interrupted := false
	func() { defer func() { interrupted = recover() != nil }(); _ = i.Discard(t.Context()) }()
	i.Close()
	fileutil.CrashPoint = previous
	if !interrupted {
		t.Fatal("cleanup did not reach receipt persistence")
	}
	if _, err := worktree.MaterializeCandidate(t.Context(), status.Source, worktree.Candidate{WorkspaceRoot: status.Candidate.WorkspaceRoot, WorktreeRoot: status.Candidate.WorktreeRoot}); err != nil {
		t.Fatal(err)
	}
	write(t, root, "a.txt", "recreated result\n")
	reopened, err := store.OpenIntegration(t.Context(), id, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Discard(t.Context()); err == nil {
		t.Fatal("old discard decision erased a recreated candidate")
	}
	if body, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(body) != "recreated result\n" {
		t.Fatalf("late work: %q %v", body, err)
	}
}
