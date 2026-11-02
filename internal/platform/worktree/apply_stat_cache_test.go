package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyRejectsStatCacheHiddenSourceEdit(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "config", "core.trustctime", "false")
	gitIn(t, root, "config", "core.checkstat", "minimal")
	writeRepoFile(t, root, "input", "old\n")
	path := filepath.Join(root, "input")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-qm", "base")
	snapshot, err := TakeSnapshot(t.Context(), opened(t, root))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := CreateCandidate(t.Context(), snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := RemoveCandidate(context.WithoutCancel(t.Context()), snapshot, candidate); err != nil {
			t.Error(err)
		}
	})
	writeRepoFile(t, candidate.WorkspaceRoot, "input", "new\n")
	tree, changes, err := CandidateTree(t.Context(), snapshot, candidate)
	if err != nil || len(changes) != 1 {
		t.Fatalf("candidate fixture: %v %v", changes, err)
	}
	writeRepoFile(t, root, "input", "usr\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if diff := gitIn(t, root, "diff", "--", "input"); diff != "" {
		t.Fatalf("fixture did not hide the user's edit: %s", diff)
	}
	if err := Apply(t.Context(), snapshot, tree, changes); !errors.Is(err, ErrWorkspaceMoved) {
		t.Errorf("stale candidate was not refused: %v", err)
	}
	if got := readRepoFile(t, root, "input"); got != "usr\n" {
		t.Fatalf("candidate overwrote the user's edit: %q", got)
	}
}
