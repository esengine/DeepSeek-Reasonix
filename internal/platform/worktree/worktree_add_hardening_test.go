package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// plantConditionalDriver commits a filtered file, then appends a conditional
// include that assigns the filter a marker-writing command. The include is
// evaluated in the checkout child's gitdir, not the source repository, so a
// listing done in the source repository never sees it.
func plantConditionalDriver(t *testing.T, repo, cond string) (marker string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	writeRepoFile(t, repo, ".gitattributes", "f.txt filter=pwn\n")
	writeRepoFile(t, repo, "f.txt", "hello\n")
	git("add", "-A")
	git("commit", "-q", "-m", "init")

	marker = filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(testenv.TempDir(t), "evil.cfg")
	if err := os.WriteFile(evil, []byte("[filter \"pwn\"]\n\tsmudge = "+payload+"\n\tclean = "+payload+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.OpenFile(filepath.Join(repo, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.WriteString("[includeIf \"" + cond + "\"]\n\tpath = " + evil + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = cfg.Close()
	return marker
}

func markerAbsent(t *testing.T, marker, when string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("%s ran a conditionally-included driver", when)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat marker: %v", err)
	}
}

// Create checks the new worktree out in a child whose gitdir is the new
// worktree, so a conditional include keyed on the new branch is invisible to a
// driver listing in the source repository. Create must populate it without
// running that driver, file intact.
func TestCreateDoesNotRunConditionallyIncludedDrivers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	marker := plantConditionalDriver(t, repo, "onbranch:reasonix/**")
	result, err := Create(context.Background(), opened(t, repo), testenv.TempDir(t))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := readRepoFile(t, result.WorktreeRoot, "f.txt"); got != "hello\n" {
		t.Fatalf("worktree f.txt = %q, want the committed bytes", got)
	}
	markerAbsent(t, marker, "Create")
}

// CreateCandidate checks a snapshot out detached; a conditional include keyed
// on the new worktree's gitdir is evaluated in that checkout child.
func TestCreateCandidateDoesNotRunConditionallyIncludedDrivers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	marker := plantConditionalDriver(t, repo, "gitdir:**/worktrees/**")
	snap, err := TakeSnapshot(context.Background(), opened(t, repo))
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	c, err := CreateCandidate(context.Background(), snap, filepath.Join(testenv.TempDir(t), "candidates"))
	if err != nil {
		t.Fatalf("CreateCandidate: %v", err)
	}
	if got := readRepoFile(t, c.WorkspaceRoot, "f.txt"); got != "hello\n" {
		t.Fatalf("candidate f.txt = %q, want the committed bytes", got)
	}
	markerAbsent(t, marker, "CreateCandidate")
}

// When populating the new worktree fails, Create leaves neither the worktree
// nor the branch its add created behind.
func TestCreateCleansUpBranchWhenPopulateFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX false binary")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("f.txt filter=req\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	// A user-scope required filter whose smudge fails: user scope stays live,
	// so the checkout inside the new worktree fails.
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[filter \"req\"]\n\tclean = cat\n\tsmudge = false\n\trequired = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)

	if _, err := Create(context.Background(), opened(t, repo), t.TempDir()); err == nil {
		t.Fatal("Create succeeded with a failing required filter, want an error")
	}
	if refs := strings.TrimSpace(git("for-each-ref", "--format=%(refname)", "refs/heads/reasonix/")); refs != "" {
		t.Fatalf("branches left behind: %s", refs)
	}
	if list := git("worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktrees left behind:\n%s", list)
	}
}
