package worktree

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestApplyPathsKeepsModeOnlyChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable mode requires POSIX filesystem")
	}
	repo, snap, c, finish := isolatedCandidate(t)
	if err := os.Chmod(filepath.Join(c.WorkspaceRoot, "a.txt"), 0755); err != nil {
		t.Fatal(err)
	}
	tree, changes := finish()
	if len(changes) != 1 {
		t.Fatalf("expected mode change: %v", changes)
	}
	if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(repo, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatal("mode-only change was silently discarded")
	}
}

func TestApplyPathsDistinguishesSymlinkFromEqualBytes(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	path := filepath.Join(c.WorkspaceRoot, "a.txt")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("one\n", path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	tree, changes := finish()
	if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(repo, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink and regular file were treated as identical")
	}
}
