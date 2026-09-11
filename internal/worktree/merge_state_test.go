package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWorktreeStateTokenBindsFileContentAndIndex(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	path := filepath.Join(repo, "README.md")
	original, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if changed == original {
		t.Fatal("filesystem content change did not change the state token")
	}
	gitTest(t, repo, "add", "README.md")
	staged, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if staged == changed {
		t.Fatal("index change did not change the state token")
	}
}

func TestWorktreeStateTokenBindsSymlinkTargetAndMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and executable-mode semantics differ on Windows")
	}
	requireGit(t)
	repo := initRepo(t)
	link := filepath.Join(repo, "link")
	if err := os.Symlink("first", link); err != nil {
		t.Fatal(err)
	}
	first, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second", link); err != nil {
		t.Fatal(err)
	}
	second, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("symlink target change did not change the state token")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "mode.txt")
	if err := os.WriteFile(file, []byte("mode\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := worktreeStateToken(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if executable == plain {
		t.Fatal("file mode change did not change the state token")
	}
}
