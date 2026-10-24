package control

import (
	"context"
	"errors"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitpush"
	"reasonix/internal/state/mutation"
)

// ErrWorkspaceNotTrusted refuses a push from a workspace the person has not
// trusted: a push is the one git action that leaves the machine.
var ErrWorkspaceNotTrusted = errors.New("control: the workspace is not trusted, so nothing is pushed from it")

// PushRequest is a push the person confirmed: the repository, and the plan the
// preview showed them. A repository that no longer matches the plan is refused.
type PushRequest struct {
	// Repo names the repository below the workspace; empty is the workspace's own.
	Repo string       `json:"repo,omitempty"`
	Plan gitpush.Plan `json:"plan"`
}

// GitPushPreview says what pushing the repository's current branch would send
// and where, without reaching the remote.
func (c *Controller) GitPushPreview(ctx context.Context, repoDir string) (gitpush.Plan, error) {
	if c.WorkspaceTrust() != config.WorkspaceTrusted {
		return gitpush.Plan{}, ErrWorkspaceNotTrusted
	}
	repo, err := c.RepoFor(ctx, repoDir)
	if err != nil {
		return gitpush.Plan{}, err
	}
	return gitpush.Preview(ctx, repo)
}

// GitPush sends the confirmed branch to its upstream. Source state is protected
// during preparation and tracking-ref settlement, not during the transfer.
func (c *Controller) GitPush(ctx context.Context, req PushRequest) (gitpush.Result, error) {
	if c.WorkspaceTrust() != config.WorkspaceTrusted {
		return gitpush.Result{}, ErrWorkspaceNotTrusted
	}
	repo, err := c.RepoFor(ctx, req.Repo)
	if err != nil {
		return gitpush.Result{}, err
	}
	if c.mutations == nil {
		c.commitMu.Lock()
		defer c.commitMu.Unlock()
		return gitpush.Push(ctx, repo, req.Plan)
	}
	return gitpush.PushWithScope(ctx, repo, req.Plan, func(ctx context.Context) (func(), error) {
		c.commitMu.Lock()
		lease, err := c.holdGitMutation(ctx, repo, "git.push", false)
		if err != nil {
			c.commitMu.Unlock()
			return nil, err
		}
		return func() {
			lease.End(mutation.Unknown)
			c.commitMu.Unlock()
		}, nil
	})
}
