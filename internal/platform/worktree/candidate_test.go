package worktree

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

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitcmd.Command(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRepoFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// A candidate starts from the workspace as it is — uncommitted edits and
// untracked files included, ignored files not — and applying its result writes
// the files without staging anything or touching refs.
func TestCandidateStartsFromTheLiveWorkspaceAndAppliesBack(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	writeRepoFile(t, repo, ".gitignore", "build/\n")
	writeRepoFile(t, repo, "a.txt", "committed\n")
	writeRepoFile(t, repo, "gone.txt", "to delete\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	writeRepoFile(t, repo, "a.txt", "edited, not committed\n")
	writeRepoFile(t, repo, "new/untracked.txt", "untracked\n")
	writeRepoFile(t, repo, "build/out.bin", "ignored\n")
	head := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD"))

	ctx := context.Background()
	snap, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	c, err := CreateCandidate(ctx, snap, filepath.Join(testenv.TempDir(t), "candidates"))
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if got := readRepoFile(t, c.WorkspaceRoot, "a.txt"); got != "edited, not committed\n" {
		t.Fatalf("candidate a.txt = %q, want the uncommitted edit", got)
	}
	if got := readRepoFile(t, c.WorkspaceRoot, "new/untracked.txt"); got != "untracked\n" {
		t.Fatalf("candidate lacks the untracked file: %q", got)
	}
	if _, err := os.Stat(filepath.Join(c.WorkspaceRoot, "build", "out.bin")); !os.IsNotExist(err) {
		t.Fatal("an ignored file reached the candidate")
	}

	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate's version\n")
	writeRepoFile(t, c.WorkspaceRoot, "added.txt", "candidate added\n")
	if err := os.Remove(filepath.Join(c.WorkspaceRoot, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	tree, changes, err := CandidateTree(ctx, snap, c)
	if err != nil {
		t.Fatalf("CandidateTree: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("changes = %+v, want a.txt, added.txt and gone.txt", changes)
	}
	if patch, err := Patch(ctx, snap, tree); err != nil || !strings.Contains(patch, "candidate's version") {
		t.Fatalf("Patch = %q, %v", patch, err)
	}
	if err := Apply(ctx, snap, tree, changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate's version\n" {
		t.Fatalf("workspace a.txt = %q after apply", got)
	}
	if got := readRepoFile(t, repo, "added.txt"); got != "candidate added\n" {
		t.Fatalf("workspace added.txt = %q after apply", got)
	}
	if _, err := os.Stat(filepath.Join(repo, "gone.txt")); !os.IsNotExist(err) {
		t.Fatal("the candidate's deletion was not applied")
	}
	if staged := strings.TrimSpace(gitIn(t, repo, "diff", "--cached", "--name-only")); staged != "" {
		t.Fatalf("apply staged %q; the user's index must stay as it was", staged)
	}
	if now := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD")); now != head {
		t.Fatal("apply moved HEAD")
	}
	if err := RemoveCandidate(ctx, snap, c); err != nil {
		t.Fatalf("RemoveCandidate: %v", err)
	}
	if _, err := os.Stat(c.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatal("candidate worktree still on disk")
	}
}

// A workspace that moved after the snapshot is not overwritten by a candidate
// that never saw the move.
func TestApplyRefusesAWorkspaceThatMoved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	writeRepoFile(t, repo, "a.txt", "one\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	ctx := context.Background()
	snap, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateCandidate(ctx, snap, filepath.Join(testenv.TempDir(t), "candidates"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = RemoveCandidate(ctx, snap, c) }()
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
	tree, changes, err := CandidateTree(ctx, snap, c)
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, "a.txt", "the user kept working\n")
	if err := Apply(ctx, snap, tree, changes); !errors.Is(err, ErrWorkspaceMoved) {
		t.Fatalf("Apply = %v, want ErrWorkspaceMoved", err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "the user kept working\n" {
		t.Fatalf("a refused apply still wrote a.txt: %q", got)
	}
}
