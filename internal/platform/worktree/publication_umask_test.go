//go:build !windows

package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationAcceptsNewEntriesCreatedUnderTheProcessUmask(t *testing.T) {
	requireGit(t)
	previous := processUmask
	processUmask = 0o002
	t.Cleanup(func() { processUmask = previous })
	repo := initRepo(t)
	ctx := context.Background()
	baseline, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo, "pkg")
	if err := os.Mkdir(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "new.go")
	if err := os.WriteFile(file, []byte("package pkg\n"), 0o664); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{dir: 0o775, file: 0o664} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	target, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicationModes(ctx, baseline, target); err != nil {
		t.Fatalf("default creation modes under umask 002 were refused: %v", err)
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	narrowed, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePublicationModes(ctx, baseline, narrowed); err == nil {
		t.Fatal("a deliberately private new file would be published with default permissions")
	}
}
