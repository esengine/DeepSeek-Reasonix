package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

// isolatedCandidate commits a.txt and b.txt, snapshots the repo, and hands back
// a candidate the test edits before calling finish for its tree and changes.
func isolatedCandidate(t *testing.T) (repo string, snap Snapshot, c Candidate, finish func() (string, []Change)) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo = testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	writeRepoFile(t, repo, "a.txt", "one\n")
	writeRepoFile(t, repo, "b.txt", "two\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	ctx := context.Background()
	var err error
	if snap, err = TakeSnapshot(ctx, opened(t, repo)); err != nil {
		t.Fatal(err)
	}
	if c, err = CreateCandidate(ctx, snap, filepath.Join(testenv.TempDir(t), "isolated")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveCandidate(ctx, snap, c) })
	return repo, snap, c, func() (string, []Change) {
		tree, changes, err := CandidateTree(ctx, snap, c)
		if err != nil {
			t.Fatal(err)
		}
		return tree, changes
	}
}

// The point of applying path by path: the workspace moved on elsewhere, and
// the candidate's own paths still land.
func TestApplyPathsLandsBesideUnrelatedWork(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
	writeRepoFile(t, c.WorkspaceRoot, "new/c.txt", "created\n")
	if err := os.Remove(filepath.Join(c.WorkspaceRoot, "b.txt")); err != nil {
		t.Fatal(err)
	}
	tree, changes := finish()
	writeRepoFile(t, repo, "unrelated.txt", "the parent kept working\n")

	if err := ApplyPaths(context.Background(), snap, tree, changes); err != nil {
		t.Fatalf("ApplyPaths = %v", err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if got := readRepoFile(t, repo, "new/c.txt"); got != "created\n" {
		t.Fatalf("new/c.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("b.txt still there: %v", err)
	}
	if got := readRepoFile(t, repo, "unrelated.txt"); got != "the parent kept working\n" {
		t.Fatalf("unrelated.txt = %q", got)
	}
}

// One conflicting path refuses the whole apply, names the path, and writes
// nothing — not even the paths that would have applied cleanly.
func TestApplyPathsRefusesAConflictAndWritesNothing(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	writeRepoFile(t, repo, "a.txt", "the parent edited it too\n")

	err := ApplyPaths(context.Background(), snap, tree, changes)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrApplyConflict) {
		t.Fatalf("ApplyPaths = %v, want a ConflictError", err)
	}
	if !slices.Equal(conflict.Paths, []string{"a.txt"}) {
		t.Fatalf("conflicting paths = %v, want [a.txt]", conflict.Paths)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
		t.Fatalf("a refused apply wrote b.txt: %q", got)
	}
}

// A path the workspace already holds in the candidate's version is not a
// conflict: applying twice, or after the same edit landed some other way, is fine.
func TestApplyPathsAcceptsAPathAlreadyAtTheTarget(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
	tree, changes := finish()
	writeRepoFile(t, repo, "a.txt", "candidate\n")

	if err := ApplyPaths(context.Background(), snap, tree, changes); err != nil {
		t.Fatalf("ApplyPaths = %v, want the matching path accepted", err)
	}
}

// A candidate rooted in a subfolder may not reach the repository around it.
func TestChangePathsRefusesAPathOutsideThePrefix(t *testing.T) {
	snap := Snapshot{RepoRoot: "/repo", Prefix: "app"}
	got, err := ChangePaths(snap, []Change{{Status: "M", Path: "app/x.go"}})
	if err != nil || len(got) != 1 || got[0] != filepath.Join("/repo", "app", "x.go") {
		t.Fatalf("ChangePaths inside = %v, %v", got, err)
	}
	for _, p := range []string{"lib/y.go", "../etc/passwd", "application/z.go"} {
		if _, err := ChangePaths(snap, []Change{{Status: "M", Path: p}}); !errors.Is(err, ErrOutsideWorkspace) {
			t.Fatalf("ChangePaths(%q) = %v, want ErrOutsideWorkspace", p, err)
		}
	}
}

func TestDiffStatCountsEachChange(t *testing.T) {
	_, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "one\nmore\nlines\n")
	tree, changes := finish()
	stats, err := DiffStat(context.Background(), snap, tree, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Path != "a.txt" || stats[0].Status != "M" || stats[0].Added != 2 || stats[0].Removed != 0 {
		t.Fatalf("stats = %+v, want a.txt M +2 -0", stats)
	}
}
