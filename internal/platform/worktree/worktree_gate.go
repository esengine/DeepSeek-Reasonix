package worktree

import (
	"context"
	"path/filepath"
	"sync"

	"reasonix/internal/platform/gitcmd"
)

// gates holds one gate per repository, keyed by its common Git directory.
var gates sync.Map

func worktreeGate(commonDir string) chan struct{} {
	gate, _ := gates.LoadOrStore(filepath.Clean(commonDir), make(chan struct{}, 1))
	return gate.(chan struct{})
}

// runWorktree runs `git worktree args...`, one at a time per repository in this
// process. An add creates a new entry's directory before it writes the entry's
// files, and every worktree command reads each entry's commondir and dies on one
// it finds empty ("failed to read .../commondir: Success").
func runWorktree(ctx context.Context, repo gitcmd.Repo, args ...string) (string, string, error) {
	gate := worktreeGate(repo.CommonDir)
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
	defer func() { <-gate }()
	return runGit(ctx, repo, append([]string{"worktree"}, args...)...)
}
