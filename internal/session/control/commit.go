package control

import (
	"context"
	"errors"
	"path/filepath"

	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/state/mutation"
)

// CommitDrafter proposes a commit message for a staged change set.
type CommitDrafter interface {
	Generate(ctx context.Context, in commitmsg.Input) (string, error)
}

// CommitProposal is the staged set as read and the message proposed for it.
// Fingerprint names that set; CommitStaged refuses an index that has moved on.
type CommitProposal struct {
	Message        string           `json:"message"`
	Fingerprint    string           `json:"fingerprint"`
	Files          []gitcommit.File `json:"files"`
	Truncated      bool             `json:"truncated"`
	ContentSecrets bool             `json:"contentSecrets"`
	// DraftFailure says why Message is empty when the model could not draft one:
	// the staged set above is still real, and the person writes the message.
	DraftFailure string `json:"draftFailure,omitempty"`
}

// Why a draft failed, named by the code that knows: the drafter's own sentinels,
// a deadline, or the model call failing for any other reason.
const (
	CommitDraftNoModel     = "no_model"
	CommitDraftTimeout     = "timeout"
	CommitDraftNoAnswer    = "no_answer"
	CommitDraftModelFailed = "model_failed"
)

func commitDraftFailure(err error) string {
	switch {
	case errors.Is(err, commitmsg.ErrUnavailable):
		return CommitDraftNoModel
	case errors.Is(err, context.DeadlineExceeded):
		return CommitDraftTimeout
	case errors.Is(err, commitmsg.ErrNoAnswer):
		return CommitDraftNoAnswer
	}
	return CommitDraftModelFailed
}

// CommitRequest is a commit the person confirmed. AcknowledgeSecrets answers
// the warning a proposal carried; it does not widen what is committed.
type CommitRequest struct {
	// Repo names the repository below the workspace; empty is the workspace's own.
	Repo               string `json:"repo,omitempty"`
	Message            string `json:"message"`
	Fingerprint        string `json:"fingerprint"`
	AcknowledgeSecrets bool   `json:"acknowledgeSecrets"`
}

// CommitResult is the commit that was recorded.
type CommitResult struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// StageRequest names the changes to move into the index, or out of it when
// Unstage is set. All means every change the workspace lists.
type StageRequest struct {
	Repo    string   `json:"repo,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	All     bool     `json:"all,omitempty"`
	Unstage bool     `json:"unstage,omitempty"`
}

// StageChanges stages or unstages the named changes. It shares the commit lock,
// so the index never moves under a commit in flight.
func (c *Controller) StageChanges(ctx context.Context, req StageRequest) error {
	repo, err := c.RepoFor(ctx, req.Repo)
	if err != nil {
		return err
	}
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	activity, err := c.holdWorkspaceActivity(ctx)
	if err != nil {
		return err
	}
	defer activity.release()
	lease, err := c.holdGitMutation(ctx, repo, "git.stage", req.All, req.Paths...)
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	if req.Unstage {
		return gitcommit.Unstage(ctx, repo, req.Paths, req.All)
	}
	return gitcommit.Stage(ctx, repo, req.Paths, req.All)
}

// ProposeCommit reads the staged changes and has the session's model propose a
// message. Nothing is staged or committed: the person edits the text first.
func (c *Controller) ProposeCommit(ctx context.Context, repoDir string) (CommitProposal, error) {
	repo, err := c.RepoFor(ctx, repoDir)
	if err != nil {
		return CommitProposal{}, err
	}
	snap, err := c.stagedSnapshot(ctx, repo)
	if err != nil {
		return CommitProposal{}, err
	}
	files := make([]commitmsg.File, len(snap.Files))
	for i, f := range snap.Files {
		files[i] = commitmsg.File{Path: f.Path, Status: f.Status}
	}
	workspace := ""
	if root := c.WorkspaceRoot(); repoDir == "" && root != "" {
		workspace = filepath.Base(root)
	} else if repoDir != "" {
		workspace = filepath.Base(repo.WorkTree)
	}
	msg, err := c.committer.Generate(ctx, commitmsg.Input{
		Files: files, Diff: snap.Diff, Truncated: snap.Truncated, Recent: snap.Recent, Workspace: workspace,
	})
	proposal := CommitProposal{
		Message: msg, Fingerprint: snap.Fingerprint, Files: snap.Files,
		Truncated: snap.Truncated || len(snap.Diff) > commitmsg.MaxDiffBytes, ContentSecrets: snap.ContentSecrets,
	}
	if err != nil {
		// The person leaving is not a failed draft: nobody is left to read it.
		if ctx.Err() != nil {
			return CommitProposal{}, err
		}
		proposal.DraftFailure = commitDraftFailure(err)
	}
	return proposal, nil
}

// CommitStaged records the confirmed message as one local commit of exactly the
// staged set the person saw.
func (c *Controller) CommitStaged(ctx context.Context, req CommitRequest) (CommitResult, error) {
	repo, err := c.RepoFor(ctx, req.Repo)
	if err != nil {
		return CommitResult{}, err
	}
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	releaseWorkspace, err := c.holdWorkspaceActivity(ctx)
	if err != nil {
		return CommitResult{}, err
	}
	defer releaseWorkspace.release()
	lease, err := c.holdGitMutation(ctx, repo, "git.commit", false)
	if err != nil {
		return CommitResult{}, err
	}
	defer lease.End(mutation.Unknown)
	res, err := gitcommit.Commit(ctx, repo, req.Message, req.Fingerprint, req.AcknowledgeSecrets)
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Hash: res.Hash, Subject: res.Subject}, nil
}
