package gitstatus

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func branchGitOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func TestSwitchBranchPreservesAnInProgressOperation(t *testing.T) {
	for _, layout := range []string{"direct", "alias", "linked-alias"} {
		for _, operation := range []string{"merge", "rebase"} {
			for _, resolved := range []bool{false, true} {
				label := operation + "/unresolved"
				if resolved {
					label = operation + "/clean-index"
				}
				t.Run(layout+"/"+label, func(t *testing.T) {
					dir := repoWithCommit(t)
					git(t, dir, "checkout", "-qb", "original")
					if layout != "direct" {
						if runtime.GOOS == "windows" {
							t.Skip("directory symlink requires privileges on Windows")
						}
						if layout == "linked-alias" {
							linked := filepath.Join(t.TempDir(), "linked")
							git(t, dir, "checkout", "--detach")
							git(t, dir, "worktree", "add", linked, "original")
							dir = linked
						}
						alias := filepath.Join(t.TempDir(), "alias")
						if err := os.Symlink(dir, alias); err != nil {
							t.Fatal(err)
						}
						dir = alias
					}
					gitDir := strings.TrimSpace(string(branchGitOutput(t, dir, "rev-parse", "--absolute-git-dir")))
					git(t, dir, "checkout", "-qb", "other")
					file := filepath.Join(dir, "a.go")
					if err := os.WriteFile(file, []byte("other branch\n"), 0600); err != nil {
						t.Fatal(err)
					}
					git(t, dir, "commit", "-qam", "other")
					git(t, dir, "checkout", "-q", "original")
					if err := os.WriteFile(file, []byte("original branch\n"), 0600); err != nil {
						t.Fatal(err)
					}
					git(t, dir, "commit", "-qam", "original")
					git(t, dir, "branch", "target")
					args := []string{operation, "other"}
					marker := filepath.Join(gitDir, "MERGE_HEAD")
					if operation == "rebase" {
						args = []string{"rebase", "--merge", "other"}
						marker = filepath.Join(gitDir, "rebase-merge", "head-name")
					}
					cmd := exec.Command("git", args...)
					cmd.Dir = dir
					cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_EDITOR=true")
					if out, err := cmd.CombinedOutput(); err == nil {
						t.Fatalf("expected %s conflict: %s", operation, out)
					}
					if resolved {
						git(t, dir, "restore", "--source=HEAD", "--staged", "--worktree", "--", "a.go")
					}
					state, err := os.ReadFile(marker)
					if err != nil {
						t.Fatal(err)
					}
					beforeHead := branchGitOutput(t, dir, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
					beforeIndex := branchGitOutput(t, dir, "ls-files", "-s", "-z")
					beforeFile, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					err = SwitchBranch(t.Context(), opened(t, dir), "target")
					wanted := ErrLocalChanges
					if resolved {
						wanted = ErrSwitchFailed
					}
					if !errors.Is(err, wanted) {
						t.Fatalf("switch during %s = %v, want %v", label, err, wanted)
					}
					if after := branchGitOutput(t, dir, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD"); !bytes.Equal(after, beforeHead) {
						t.Error("refused switch changed HEAD")
					}
					if after := branchGitOutput(t, dir, "ls-files", "-s", "-z"); !bytes.Equal(after, beforeIndex) {
						t.Error("refused switch changed index")
					}
					if after, err := os.ReadFile(file); err != nil || !bytes.Equal(after, beforeFile) {
						t.Errorf("refused switch changed conflict file: %v", err)
					}
					if after, err := os.ReadFile(marker); err != nil || !bytes.Equal(after, state) {
						t.Errorf("refused switch changed operation state: %v", err)
					}
				})
			}
		}
	}
}

func TestSwitchBranchPreservesSubmoduleAndRefusesMovedPointer(t *testing.T) {
	source := repoWithCommit(t)
	first := strings.TrimSpace(string(branchGitOutput(t, source, "rev-parse", "HEAD")))
	dir := repoWithCommit(t)
	git(t, dir, "checkout", "-qb", "original")
	git(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", source, "deps/lib")
	git(t, dir, "commit", "-qam", "pin first submodule commit")
	if err := os.WriteFile(filepath.Join(source, "a.go"), []byte("second submodule commit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, source, "commit", "-qam", "second")
	second := strings.TrimSpace(string(branchGitOutput(t, source, "rev-parse", "HEAD")))
	submodule := filepath.Join(dir, "deps", "lib")
	git(t, submodule, "-c", "protocol.file.allow=always", "fetch", "origin")
	git(t, dir, "checkout", "-qb", "next")
	git(t, submodule, "checkout", "-q", "--detach", second)
	git(t, dir, "add", "deps/lib")
	git(t, dir, "commit", "-qm", "pin second submodule commit")
	git(t, dir, "checkout", "-q", "original")
	git(t, submodule, "checkout", "-q", "--detach", first)
	repo := opened(t, dir)
	if err := SwitchBranch(t.Context(), repo, "next"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(branchGitOutput(t, submodule, "rev-parse", "HEAD"))); got != first {
		t.Fatalf("switch recursively moved submodule: %s", got)
	}
	beforeHead := branchGitOutput(t, dir, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD")
	beforeIndex := branchGitOutput(t, dir, "ls-files", "-s", "-z")
	if err := SwitchBranch(t.Context(), repo, "original"); !errors.Is(err, ErrLocalChanges) {
		t.Fatalf("moved submodule pointer = %v, want ErrLocalChanges", err)
	}
	if after := branchGitOutput(t, dir, "rev-parse", "HEAD", "--symbolic-full-name", "HEAD"); !bytes.Equal(after, beforeHead) {
		t.Error("refused switch moved HEAD")
	}
	if after := branchGitOutput(t, dir, "ls-files", "-s", "-z"); !bytes.Equal(after, beforeIndex) {
		t.Error("refused switch changed gitlink")
	}
	if got := strings.TrimSpace(string(branchGitOutput(t, submodule, "rev-parse", "HEAD"))); got != first {
		t.Fatalf("refused switch moved submodule: %s", got)
	}
}
