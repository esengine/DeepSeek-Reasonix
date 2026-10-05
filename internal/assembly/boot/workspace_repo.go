package boot

import (
	"context"
	"path/filepath"
	"time"

	"reasonix/internal/platform/environment"
	"reasonix/internal/platform/gitcmd"
)

// workspaceRepoTimeout bounds the one git call a session open makes.
const workspaceRepoTimeout = 10 * time.Second

// workspaceRepo is root's git identity for the session being built, settled
// before the session runs any command. A rebuild of the same workspace keeps
// the identity its session opened with instead of rediscovering it from files
// that session could since have written.
func workspaceRepo(ctx context.Context, carried gitcmd.Repo, root string) gitcmd.Repo {
	abs, err := filepath.Abs(root)
	if err != nil {
		return gitcmd.Repo{Dir: root}
	}
	if carried.Dir != "" && filepath.Clean(carried.Dir) == abs {
		return carried
	}
	if environment.WorkspaceVCS(abs) != "git" {
		return gitcmd.Repo{Dir: abs}
	}
	ctx, cancel := context.WithTimeout(ctx, workspaceRepoTimeout)
	defer cancel()
	repo, err := gitcmd.Open(ctx, abs)
	if err != nil {
		return gitcmd.Repo{Dir: abs}
	}
	return repo
}
