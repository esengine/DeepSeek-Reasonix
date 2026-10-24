package control

import (
	"context"
	"crypto/rand"
	"path/filepath"

	"reasonix/internal/platform/environment"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func (c *Controller) acquireMutation(ctx context.Context, intent mutation.Intent) (*mutation.Lease, error) {
	if c.mutations == nil {
		return nil, nil
	}
	return c.mutations.Acquire(ctx, c.mutationIntent(intent))
}

func (c *Controller) tryAcquireMutation(ctx context.Context, intent mutation.Intent) (*mutation.Lease, error) {
	if c.mutations == nil {
		return nil, nil
	}
	return c.mutations.TryAcquire(ctx, c.mutationIntent(intent))
}

func (c *Controller) mutationIntent(intent mutation.Intent) mutation.Intent {
	intent.SessionID = sessionstore.BranchID(c.SessionPath())
	intent.ExecutionID = "control_" + rand.Text()
	return intent
}

func (c *Controller) holdGitMutation(ctx context.Context, repo gitcmd.Repo, call string, all bool, paths ...string) (*mutation.Lease, error) {
	return c.holdGitMutationExpected(ctx, repo, call, all, nil, paths...)
}

func (c *Controller) holdGitMutationExpected(ctx context.Context, repo gitcmd.Repo, call string, all bool, expected map[string]mutation.ContentVersion, paths ...string) (*mutation.Lease, error) {
	return c.holdGitMutationUsing(ctx, repo, call, all, expected, c.acquireMutation, paths...)
}

func (c *Controller) tryHoldGitMutation(ctx context.Context, repo gitcmd.Repo, call string, all bool, paths ...string) (*mutation.Lease, error) {
	return c.holdGitMutationUsing(ctx, repo, call, all, nil, c.tryAcquireMutation, paths...)
}

func (c *Controller) holdGitMutationUsing(ctx context.Context, repo gitcmd.Repo, call string, all bool, expected map[string]mutation.ContentVersion, acquire func(context.Context, mutation.Intent) (*mutation.Lease, error), paths ...string) (*mutation.Lease, error) {
	if c.mutations == nil {
		return nil, nil
	}
	resources := []string{repo.GitDir, repo.CommonDir}
	if all {
		resources = append(resources, repo.WorkTree)
	} else {
		for _, path := range paths {
			if !filepath.IsLocal(path) {
				return nil, gitcommit.ErrUnknownPath
			}
			resources = append(resources, filepath.Join(repo.Dir, filepath.FromSlash(path)))
		}
	}
	lease, err := acquire(ctx, mutation.Intent{Effect: mutation.GitMetadata, Paths: resources, CallID: call, Expected: expected})
	if err != nil {
		return nil, err
	}
	if err := repo.Verify(); err != nil {
		lease.End(mutation.NotExecuted)
		return nil, err
	}
	return lease, nil
}

func (c *Controller) stagedSnapshot(ctx context.Context, repo gitcmd.Repo) (gitcommit.Snapshot, error) {
	lease, err := c.holdGitMutation(ctx, repo, "git.staged_snapshot", false)
	if err != nil {
		return gitcommit.Snapshot{}, err
	}
	defer lease.End(mutation.Unknown)
	return gitcommit.Staged(ctx, repo)
}

func (c *Controller) acquireUserShellMutation(ctx context.Context, call string) (*mutation.Lease, error) {
	if c.mutations == nil {
		return nil, nil
	}
	roots, err := c.WorkspaceRoots()
	if err != nil {
		return nil, err
	}
	paths := append([]string(nil), roots.Extra...)
	var repos []gitcmd.Repo
	for _, root := range roots.Extra {
		if environment.WorkspaceVCS(root) != "git" {
			continue
		}
		repo, err := gitcmd.Open(ctx, root)
		if err != nil {
			return nil, err
		}
		paths = append(paths, repo.GitDir, repo.CommonDir)
		repos = append(repos, repo)
	}
	lease, err := c.acquireMutation(ctx, mutation.Intent{Effect: mutation.Workspace, Paths: paths, CallID: call})
	if err != nil {
		return nil, err
	}
	for _, repo := range repos {
		if err := repo.VerifyDiscovery(ctx); err != nil {
			lease.End(mutation.NotExecuted)
			return nil, err
		}
	}
	return lease, nil
}
