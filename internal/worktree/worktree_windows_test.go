//go:build windows

package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateChecksOutLongRepositoryPathOnWindows(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	relativeDir := ""
	for len(filepath.Join(repo, relativeDir, "deep-file.txt")) <= 320 {
		relativeDir = filepath.Join(relativeDir, "deep-repository-segment")
	}
	relativePath := filepath.Join(relativeDir, "deep-file.txt")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, relativePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, relativePath), []byte("long path\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.longpaths=true", "-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Reasonix Test", "GIT_AUTHOR_EMAIL=reasonix@example.invalid",
			"GIT_COMMITTER_NAME=Reasonix Test", "GIT_COMMITTER_EMAIL=reasonix@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("add", "--", relativePath)
	git("commit", "-m", "add long path")

	result, err := Create(context.Background(), repo, t.TempDir())
	if err != nil {
		t.Fatalf("Create() with a long repository path: %v", err)
	}
	checkedOutPath := filepath.Join(result.WorktreeRoot, relativePath)
	if len(checkedOutPath) <= 260 {
		t.Fatalf("checked-out path length = %d, want a path beyond MAX_PATH: %q", len(checkedOutPath), checkedOutPath)
	}
	got, err := os.ReadFile(checkedOutPath)
	if err != nil {
		t.Fatalf("long-path worktree missing committed file: %v", err)
	}
	if strings.TrimSpace(string(got)) != "long path" {
		t.Fatalf("long-path worktree file = %q", got)
	}
}
