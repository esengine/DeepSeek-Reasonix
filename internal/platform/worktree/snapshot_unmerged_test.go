package worktree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotRecordsAWorkspaceMidConflict(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		return cmd.Run()
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := git("checkout", "-q", "-b", "other"); err != nil {
		t.Fatal(err)
	}
	write("other\n")
	if err := git("commit", "-qam", "other"); err != nil {
		t.Fatal(err)
	}
	if err := git("checkout", "-q", "-"); err != nil {
		t.Fatal(err)
	}
	write("mine\n")
	if err := git("commit", "-qam", "mine"); err != nil {
		t.Fatal(err)
	}
	if err := git("merge", "-q", "other"); err == nil {
		t.Fatal("fixture merge did not conflict")
	}
	index := filepath.Join(repo, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := TakeSnapshot(t.Context(), opened(t, repo))
	if err != nil {
		t.Fatalf("a conflicted index blocked the snapshot: %v", err)
	}
	out, err := exec.Command("git", "-C", repo, "cat-file", "-p", snap.Tree+":README.md").Output()
	if err != nil || !strings.Contains(string(out), "<<<<<<<") {
		t.Fatalf("snapshot did not record the working file: %q %v", out, err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("snapshot rewrote the user's conflicted index: %v", err)
	}
}
