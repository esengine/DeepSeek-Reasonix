package gitstatus

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

// opened is dir's identity as a session opening it resolves it; a directory
// outside any work tree yields the unresolved Repo a session would hold.
func opened(t *testing.T, dir string) gitcmd.Repo {
	t.Helper()
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		return gitcmd.Repo{Dir: dir}
	}
	return repo
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// The case the panel got wrong: a file created and then removed leaves two tool
// events behind and nothing on disk, so it must not stay pending.
func TestCreatedThenDeletedIsNotAChange(t *testing.T) {
	dir := testenv.TempDir(t)
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")

	scratch := filepath.Join(dir, "scratch.go")
	if err := os.WriteFile(scratch, []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes, ok, err := Status(context.Background(), opened(t, dir))
	if err != nil || !ok {
		t.Fatalf("Status = %v ok=%v", err, ok)
	}
	if len(changes) != 1 || changes[0].Path != "scratch.go" || !changes[0].Added() {
		t.Fatalf("after create: %+v, want one added scratch.go", changes)
	}

	if err := os.Remove(scratch); err != nil {
		t.Fatal(err)
	}
	changes, _, err = Status(context.Background(), opened(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("after delete: %+v, want no pending change", changes)
	}

	// And a tracked file removed by a shell command is a real pending deletion.
	if err := os.Remove(filepath.Join(dir, "keep.go")); err != nil {
		t.Fatal(err)
	}
	changes, _, err = Status(context.Background(), opened(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Deleted() {
		t.Fatalf("after removing a tracked file: %+v, want one deletion", changes)
	}
}

func TestNonRepoReportsFallbackNotFailure(t *testing.T) {
	if _, ok, err := Status(context.Background(), opened(t, testenv.TempDir(t))); ok || err != nil {
		t.Fatalf("Status(non-repo) = ok=%v err=%v, want ok=false with no error", ok, err)
	}
}

func repoWithCommit(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")
	return dir
}

func TestDiffReportsATrackedEdit(t *testing.T) {
	dir := repoWithCommit(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc B() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, truncated, err := Diff(context.Background(), opened(t, dir), "a.go")
	if err != nil || truncated {
		t.Fatalf("Diff: %v truncated=%v", err, truncated)
	}
	if !strings.Contains(text, "-func A() {}") || !strings.Contains(text, "+func B() {}") {
		t.Fatalf("both sides of the edit should be in the diff:\n%s", text)
	}
}

// Staged and unstaged are one change to a reader, so the comparison is against
// HEAD rather than the index.
func TestDiffIncludesAStagedEdit(t *testing.T) {
	dir := repoWithCommit(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc C() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "a.go")
	text, _, err := Diff(context.Background(), opened(t, dir), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "+func C() {}") {
		t.Fatalf("a staged edit should still show:\n%s", text)
	}
}

// An untracked file has nothing in HEAD to compare with. Diffing it against the
// null device is the only way to print it without first writing to the index.
func TestDiffShowsAnUntrackedFileAsAnAddition(t *testing.T) {
	dir := repoWithCommit(t)
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package a\n\nfunc N() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, _, err := Diff(context.Background(), opened(t, dir), "new.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "+func N() {}") {
		t.Fatalf("an untracked file should read as an addition:\n%s", text)
	}
}

// The path arrives from a client, so these are the three shapes that must not
// reach git: outside the tree, absolute, and readable as an option.
func TestDiffRefusesAPathThatIsNotInTheTree(t *testing.T) {
	dir := repoWithCommit(t)
	outside := filepath.Join(dir, "..", "secret.txt")
	if err := os.WriteFile(outside, []byte("private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret.txt", "a/../../secret.txt", outside, "--output=/tmp/x", ""} {
		text, _, err := Diff(context.Background(), opened(t, dir), path)
		if !errors.Is(err, ErrPathOutsideTree) {
			t.Fatalf("%q: want ErrPathOutsideTree, got %v (%q)", path, err, text)
		}
	}
}

func TestDiffCapsWhatItReturns(t *testing.T) {
	dir := repoWithCommit(t)
	big := make([]byte, 0, MaxDiffBytes*2)
	for len(big) < MaxDiffBytes*2 {
		big = append(big, []byte("a line that is long enough to add up quickly\n")...)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	text, truncated, err := Diff(context.Background(), opened(t, dir), "big.txt")
	if err != nil || !truncated || len(text) != MaxDiffBytes {
		t.Fatalf("want a truncated %d-byte answer, got %d truncated=%v err=%v", MaxDiffBytes, len(text), truncated, err)
	}
}
