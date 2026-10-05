package boot

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

// A session resolves its workspace's repository when it opens; a rebuild of
// the same workspace keeps that identity, and another workspace resolves its own.
func TestWorkspaceRepoResolvedAtOpenAndCarriedAcrossRebuilds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := testenv.TempDir(t)
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	ctx := context.Background()
	repo := workspaceRepo(ctx, gitcmd.Repo{}, root)
	if !repo.Valid() {
		t.Fatalf("workspaceRepo(%s) = %+v, want a resolved repository", root, repo)
	}
	carried := gitcmd.Repo{Dir: repo.Dir, GitDir: "/pinned", CommonDir: "/pinned", WorkTree: "/pinned"}
	if got := workspaceRepo(ctx, carried, root); got != carried {
		t.Fatalf("rebuild of the same workspace = %+v, want the identity it opened with", got)
	}
	other := testenv.TempDir(t)
	if got := workspaceRepo(ctx, carried, other); got.Valid() || got.Dir != filepath.Clean(other) {
		t.Fatalf("another workspace = %+v, want it resolved on its own (no repository)", got)
	}
}
