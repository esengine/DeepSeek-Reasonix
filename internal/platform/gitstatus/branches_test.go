package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

// branchesWithCommits is a repository with two extra locals. "next" moves the
// file a commit of its own, so an uncommitted edit to it cannot ride along —
// which is the case the local-changes refusal exists for.
func branchesWithCommits(t *testing.T) string {
	t.Helper()
	dir := repoWithCommit(t)
	git(t, dir, "checkout", "-q", "-b", "next")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc B() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "commit", "-qam", "next")
	git(t, dir, "branch", "topic")
	git(t, dir, "checkout", "-q", "-")
	return dir
}

func TestBranchesListLocalsAndMarkCurrent(t *testing.T) {
	dir := branchesWithCommits(t)
	here, ok := Summary(context.Background(), opened(t, dir))
	if !ok || here.Detached {
		t.Fatalf("summary: %+v ok=%v", here, ok)
	}
	list, ok, err := Branches(context.Background(), opened(t, dir))
	if err != nil || !ok {
		t.Fatalf("Branches: %v ok=%v", err, ok)
	}
	marked := 0
	var names []string
	for _, b := range list {
		names = append(names, b.Name)
		if !b.Current {
			continue
		}
		marked++
		if b.Name != here.Branch {
			t.Fatalf("the marked branch is %q, the summary says %q", b.Name, here.Branch)
		}
		if b.Worktree != "" {
			t.Fatalf("the branch held here must not read as a blocker: %+v", b)
		}
	}
	if marked != 1 || len(list) != 3 {
		t.Fatalf("want three locals with exactly one current, got %d marked of %v", marked, names)
	}
}

// A branch another linked worktree holds is listed with where it lives, so a
// menu can close the row before git has to say so.
func TestBranchesNameTheWorktreeThatHoldsABranch(t *testing.T) {
	dir := branchesWithCommits(t)
	other := worktreeBeside(t, dir)
	git(t, dir, "worktree", "add", other, "topic")

	list, ok, err := Branches(context.Background(), opened(t, dir))
	if err != nil || !ok {
		t.Fatalf("Branches: %v ok=%v", err, ok)
	}
	var busy Branch
	for _, b := range list {
		if b.Name == "topic" {
			busy = b
		}
	}
	if !sameTree(busy.Worktree, other) {
		t.Fatalf("topic is held at %q, want %q (%+v)", busy.Worktree, other, list)
	}
}

func TestSwitchBranchMovesHeadAndKeepsChanges(t *testing.T) {
	dir := branchesWithCommits(t)
	if err := os.WriteFile(filepath.Join(dir, "scratch.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SwitchBranch(context.Background(), opened(t, dir), "topic"); err != nil {
		t.Fatalf("SwitchBranch: %v", err)
	}
	s, ok := Summary(context.Background(), opened(t, dir))
	if !ok || s.Branch != "topic" || s.Detached {
		t.Fatalf("after switch: %+v ok=%v", s, ok)
	}
	// Uncommitted work rides along; a switch that ate it would be a data loss.
	if s.Untracked != 1 {
		t.Fatalf("the untracked file should survive: %+v", s)
	}
	// Switching to the branch already held answers as success.
	if err := SwitchBranch(context.Background(), opened(t, dir), "topic"); err != nil {
		t.Fatalf("switch to the held branch: %v", err)
	}
}

func TestSwitchBranchRefusalsAreClassified(t *testing.T) {
	dir := branchesWithCommits(t)

	// Uncommitted changes to a tracked file are refused with the reason that
	// says what to do: commit or stash first.
	blocker := filepath.Join(dir, "a.go")
	if err := os.WriteFile(blocker, []byte("package a\n\nfunc Changed() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SwitchBranch(context.Background(), opened(t, dir), "next")
	if !errors.Is(err, ErrLocalChanges) {
		t.Fatalf("conflicting work: want ErrLocalChanges, got %v", err)
	}

	// With the tree clean again, the only thing left to refuse is the branch
	// another worktree holds.
	if err := os.WriteFile(blocker, []byte("package a\n\nfunc A() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := worktreeBeside(t, dir)
	git(t, dir, "worktree", "add", other, "topic")
	err = SwitchBranch(context.Background(), opened(t, dir), "topic")
	if !errors.Is(err, ErrBranchBusy) {
		t.Fatalf("branch held elsewhere: want ErrBranchBusy, got %v", err)
	}

	// Nothing by that name.
	err = SwitchBranch(context.Background(), opened(t, dir), "no-such-branch")
	if !errors.Is(err, ErrBranchUnknown) {
		t.Fatalf("unknown branch: want ErrBranchUnknown, got %v", err)
	}

	// Names that read as options never reach git as one.
	for _, name := range []string{"", "  ", "-C", "--help"} {
		var invalid *InvalidNameError
		err = SwitchBranch(context.Background(), opened(t, dir), name)
		if !errors.As(err, &invalid) {
			t.Fatalf("%q: want InvalidNameError, got %v", name, err)
		}
	}
}

// An untracked file is only a blocker when the target would write the same
// path; every other untracked path rides along.
func TestSwitchBranchRefusesAnUntrackedPathTheTargetHolds(t *testing.T) {
	dir := branchesWithCommits(t)
	git(t, dir, "checkout", "-q", "next")
	held := filepath.Join(dir, "held.txt")
	if err := os.WriteFile(held, []byte("next's version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "held.txt")
	git(t, dir, "commit", "-qm", "next holds held.txt")
	git(t, dir, "checkout", "-q", "-")

	if err := os.WriteFile(held, []byte("untracked squatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SwitchBranch(context.Background(), opened(t, dir), "next")
	if !errors.Is(err, ErrLocalChanges) {
		t.Fatalf("untracked path the target holds: want ErrLocalChanges, got %v", err)
	}

	// A path the target never heard of is not a blocker.
	other := filepath.Join(dir, "mine.txt")
	if err := os.WriteFile(other, []byte("untracked elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(held); err != nil {
		t.Fatal(err)
	}
	if err := SwitchBranch(context.Background(), opened(t, dir), "next"); err != nil {
		t.Fatalf("untracked path the target does not hold: %v", err)
	}
}

func TestSwitchBranchPreservesLocalPathCollisions(t *testing.T) {
	for _, tt := range []struct {
		name       string
		localPath  string
		targetPath string
		ignored    bool
		refuse     bool
	}{
		{"ignored file", ".env", ".env", true, true},
		{"ignored directory", "cache/nested/token", "cache", true, true},
		{"ignored parent file", "secrets", "secrets/config.json", true, true},
		{"ignored nested file", "cache/nested/token", "cache/nested/token", true, true},
		{"untracked directory", "cache/nested/token", "cache", false, true},
		{"untracked parent file", "secrets", "secrets/config.json", false, true},
		{"path named like branch", "next", "next", false, true},
		{"unrelated ignored file", ".env", "next.txt", true, false},
		{"unrelated ignored sibling", "cache/private.env", "cache/public.txt", true, false},
		{"unrelated ignored prefix", "secrets", "secrets-public", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := repoWithCommit(t)
			if tt.ignored {
				if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(".env\ncache\nsecrets\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			git(t, dir, "checkout", "-qb", "next")
			targetPath := filepath.Join(dir, filepath.FromSlash(tt.targetPath))
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(targetPath, []byte("next's version\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "add", "-f", "--", tt.targetPath)
			git(t, dir, "commit", "-qm", "target holds path")
			git(t, dir, "checkout", "-q", "-")

			localPath := filepath.Join(dir, filepath.FromSlash(tt.localPath))
			localBytes := []byte("local data\x00must survive\n")
			if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(localPath, localBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			repo := opened(t, dir)
			before, err := repo.Command(context.Background(), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			err = SwitchBranch(context.Background(), repo, "next")
			if tt.refuse && !errors.Is(err, ErrLocalChanges) {
				t.Errorf("want ErrLocalChanges, got %v", err)
			} else if !tt.refuse && err != nil {
				t.Errorf("unrelated local path blocked switch: %v", err)
			}
			got, err := os.ReadFile(localPath)
			if err != nil || !bytes.Equal(got, localBytes) {
				t.Errorf("local bytes changed: got %q, err %v", got, err)
			}
			after, err := repo.Command(context.Background(), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			if tt.refuse && !bytes.Equal(after, before) {
				t.Errorf("refused switch moved HEAD: before %q, after %q", before, after)
			} else if !tt.refuse && bytes.Equal(after, before) {
				t.Error("successful switch did not move HEAD")
			}
		})
	}
}

func TestBranchPreflightFailsWhenTargetCannotBeRead(t *testing.T) {
	dir := repoWithCommit(t)
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("local data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preFlight(context.Background(), opened(t, dir), "missing"); err == nil {
		t.Fatal("unreadable target must not pass preflight")
	}
}

func TestSwitchBranchPreservesIgnoredFileCreatedAfterPreflight(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git wrapper uses POSIX shell")
	}
	dir := repoWithCommit(t)
	git(t, dir, "checkout", "-qb", "next")
	localPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(localPath, []byte("next's version\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".env")
	git(t, dir, "commit", "-qm", "target holds .env")
	git(t, dir, "checkout", "-q", "-")
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte(".env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := opened(t, dir)
	before, err := repo.Command(context.Background(), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := testenv.TempDir(t)
	wrapper := "#!/bin/sh\nfor arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = switch ]; then\n" +
		"    printf 'late local data\\n' > \"$BRANCH_SWITCH_LOCAL_FILE\"\n    break\n  fi\ndone\n" +
		"exec \"$BRANCH_SWITCH_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRANCH_SWITCH_REAL_GIT", realGit)
	t.Setenv("BRANCH_SWITCH_LOCAL_FILE", localPath)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := SwitchBranch(context.Background(), repo, "next"); !errors.Is(err, ErrSwitchFailed) {
		t.Errorf("late ignored collision: want ErrSwitchFailed, got %v", err)
	}
	got, err := os.ReadFile(localPath)
	if err != nil || string(got) != "late local data\n" {
		t.Errorf("late local bytes changed: got %q, err %v", got, err)
	}
	after, err := repo.Command(context.Background(), "rev-parse", "HEAD", "--symbolic-full-name", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("refused switch moved HEAD: before %q, after %q", before, after)
	}
}

// A directory outside any work tree has nothing to switch, and that is a
// fallback answer rather than a failure — the same contract Status keeps.
func TestBranchesOfANonRepositoryAreNotOK(t *testing.T) {
	dir := testenv.TempDir(t)
	if _, ok, err := Branches(context.Background(), opened(t, dir)); ok || err != nil {
		t.Fatalf("Branches(non-repo) = ok=%v err=%v", ok, err)
	}
	if err := SwitchBranch(context.Background(), opened(t, dir), "main"); err == nil {
		t.Fatal("switching outside a repository should refuse")
	}
}

// worktreeBeside is a sibling directory outside the repository, which is where
// a linked worktree has to live to not read as one of the tree's files.
func worktreeBeside(t *testing.T, dir string) string {
	t.Helper()
	other := filepath.Join(filepath.Dir(dir), "wt-"+filepath.Base(dir))
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	return other
}

func TestBranchesKeepLocalNameWhenATagHasTheSameName(t *testing.T) {
	dir := branchesWithCommits(t)
	git(t, dir, "tag", "next")
	list, ok, err := Branches(t.Context(), opened(t, dir))
	if err != nil || !ok {
		t.Fatalf("Branches: %v %v", ok, err)
	}
	found := false
	for _, branch := range list {
		if branch.Name == "next" {
			found = true
		}
		if branch.Name == "heads/next" {
			t.Error("tag ambiguity changed the local branch's name")
		}
	}
	if !found {
		t.Fatal("local next branch is missing")
	}
	if err := SwitchBranch(t.Context(), opened(t, dir), "next"); err != nil {
		t.Fatal(err)
	}
	current, ok := Summary(t.Context(), opened(t, dir))
	if !ok || current.Branch != "next" {
		t.Fatalf("after switch = %+v, %v", current, ok)
	}
}
