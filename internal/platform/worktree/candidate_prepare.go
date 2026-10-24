package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// PlanCandidate allocates a private record directory before Git changes shared state.
func PlanCandidate(snap Snapshot, managedRoot string) (Candidate, error) {
	if err := os.MkdirAll(managedRoot, 0700); err != nil {
		return Candidate{}, err
	}
	dir, err := os.MkdirTemp(managedRoot, "candidate-")
	if err != nil {
		return Candidate{}, err
	}
	root := filepath.Join(dir, safePathComponent(filepath.Base(snap.RepoRoot)))
	c := Candidate{WorkspaceRoot: root, WorktreeRoot: root}
	if snap.Prefix != "" {
		c.WorkspaceRoot = filepath.Join(root, filepath.FromSlash(snap.Prefix))
	}
	return c, nil
}

// MaterializeCandidate populates a planned path once. Failure retains partial state.
func MaterializeCandidate(ctx context.Context, snap Snapshot, planned Candidate) (Candidate, error) {
	source, err := snap.repo(ctx)
	if err != nil {
		return planned, err
	}
	if _, err := os.Lstat(planned.WorktreeRoot); !os.IsNotExist(err) {
		return planned, fmt.Errorf("candidate path must not exist: %s", planned.WorktreeRoot)
	}
	// Population inside the new worktree uses its own driver and includeIf policy.
	if _, stderr, err := runGit(ctx, source, "worktree", "add", "--no-checkout", "--detach", planned.WorktreeRoot, snap.Commit); err != nil {
		return planned, fmt.Errorf("create candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	repo, err := gitcmd.Open(ctx, planned.WorktreeRoot)
	if err != nil {
		return planned, fmt.Errorf("resolve candidate worktree: %w", err)
	}
	planned.Repo = repo
	if _, stderr, err := runGit(ctx, repo, "reset", "--hard", snap.Commit); err != nil {
		return planned, fmt.Errorf("populate candidate worktree: %w%s", err, stderrSuffix(stderr))
	}
	if err := applySnapshotMetadata(ctx, snap, planned); err != nil {
		return planned, fmt.Errorf("preserve snapshot filesystem modes: %w", err)
	}
	return planned, nil
}

func candidateRegistered(ctx context.Context, source gitcmd.Repo, root string) (bool, error) {
	listed, stderr, err := runGit(ctx, source, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("list candidate worktrees: %w%s", err, stderrSuffix(stderr))
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(root)); err == nil {
		root = filepath.Join(parent, filepath.Base(root))
	}
	root = filepath.ToSlash(filepath.Clean(root))
	for record := range strings.SplitSeq(listed, "\x00") {
		path, ok := strings.CutPrefix(record, "worktree ")
		if !ok {
			continue
		}
		path = filepath.ToSlash(filepath.Clean(path))
		if path == root || (runtime.GOOS == "windows" && strings.EqualFold(path, root)) {
			return true, nil
		}
	}
	return false, nil
}
