package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/platform/gitcmd"
)

// partialClone returns a blob-filtered clone of a three-commit repository, so
// the blobs of every commit but the checked-out one are absent locally.
func partialClone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(src, "init", "-q")
	for _, body := range []string{"1\n", "2\n", "3\n"} {
		if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		run(src, "add", "a.txt")
		run(src, "commit", "-q", "-m", body)
	}
	run(src, "config", "uploadpack.allowFilter", "true")
	clone := filepath.Join(t.TempDir(), "partial")
	run(src, "clone", "-q", "--filter=blob:none", "file://"+src, clone)
	return clone
}

// Reviewing a commit whose objects a partial clone never fetched fails with an
// error that names that cause, not an empty diff or an opaque exit status.
func TestReviewCommitInPartialCloneReportsObjectNotLocal(t *testing.T) {
	t.Chdir(partialClone(t))
	if _, err := getReviewDiff("", "HEAD~1"); !errors.Is(err, gitcmd.ErrObjectNotLocal) {
		t.Fatalf("getReviewDiff = %v, want ErrObjectNotLocal", err)
	}
	if _, err := getReviewDiff("", "no-such-rev"); err == nil || errors.Is(err, gitcmd.ErrObjectNotLocal) {
		t.Fatalf("getReviewDiff(no-such-rev) = %v, want an ordinary failure", err)
	}
}
