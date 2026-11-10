package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCreateFromBranchKeepsSourceAndSeparatesTasks(t *testing.T) {
	requireGit(t)
	dir := initRepo(t)
	gitIn(t, dir, "config", "user.name", "")
	gitIn(t, dir, "config", "user.email", "")
	repo := opened(t, dir)
	ctx := context.Background()
	base, _, _ := runGit(ctx, repo, "rev-parse", "HEAD")
	if _, _, err := runGit(ctx, repo, "branch", "task-base"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runGit(ctx, repo, "update-ref", "refs/remotes/origin/main", strings.TrimSpace(base)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("new current head\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-am", "advance current branch")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("uncommitted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	managed := testenv.TempDir(t)
	first, err := CreateFrom(ctx, repo, managed, "refs/heads/task-base")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateFrom(ctx, repo, managed, "refs/remotes/origin/main")
	if err != nil {
		t.Fatal(err)
	}
	if first.Head != strings.TrimSpace(base) || first.Branch == second.Branch || first.WorkspaceRoot == second.WorkspaceRoot {
		t.Fatalf("not independent: %+v %+v", first, second)
	}
	for _, root := range []string{first.WorkspaceRoot, second.WorkspaceRoot} {
		data, err := os.ReadFile(filepath.Join(root, "README.md"))
		// Git for Windows may check the committed LF out as CRLF (core.autocrlf).
		if err != nil || strings.TrimSpace(string(data)) != "base" {
			t.Fatalf("checkout = %q, %v", data, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(data) != "uncommitted\n" {
		t.Fatal("source changed")
	}
	list, err := Sources(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, b := range list {
		found[b.Ref] = true
	}
	if !found["refs/heads/task-base"] || !found["refs/remotes/origin/main"] {
		t.Fatalf("sources = %+v", list)
	}
}

func TestCreateFromRejectsUnknownOrNonBranchBeforeMutation(t *testing.T) {
	requireGit(t)
	repo := opened(t, initRepo(t))
	for _, ref := range []string{"missing", "--detach", "HEAD~0", "refs/heads/missing"} {
		t.Run(ref, func(t *testing.T) {
			managed := filepath.Join(testenv.TempDir(t), "not-created")
			if _, err := CreateFrom(context.Background(), repo, managed, ref); err == nil {
				t.Fatal("invalid source accepted")
			}
			if _, err := os.Stat(managed); !os.IsNotExist(err) {
				t.Fatalf("created storage before validating source: %v", err)
			}
		})
	}
}

func TestCreateFromDetachedHEADAndOccupiedBranch(t *testing.T) {
	requireGit(t)
	repo := opened(t, initRepo(t))
	ctx := t.Context()
	branch, _, err := runGit(ctx, repo, "symbolic-ref", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFrom(ctx, repo, testenv.TempDir(t), strings.TrimSpace(branch)); err != nil {
		t.Fatalf("branch occupied by source worktree: %v", err)
	}
	if _, _, err := runGit(ctx, repo, "checkout", "--detach"); err != nil {
		t.Fatal(err)
	}
	managedRoot := testenv.TempDir(t)
	result, err := CreateFrom(ctx, repo, managedRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := DiscardUnpublished(ctx, repo, managedRoot, result); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatal("unpublished worktree retained")
	}
	if _, err := os.Stat(filepath.Dir(result.WorktreeRoot)); err != nil {
		t.Fatalf("rollback touched the storage container outside the worktree: %v", err)
	}
}
