package control

import (
	"context"
	"path/filepath"

	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/runtime/commitmsg"
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
}

// CommitRequest is a commit the person confirmed. AcknowledgeSecrets answers
// the warning a proposal carried; it does not widen what is committed.
type CommitRequest struct {
	Message            string `json:"message"`
	Fingerprint        string `json:"fingerprint"`
	AcknowledgeSecrets bool   `json:"acknowledgeSecrets"`
}

// CommitResult is the commit that was recorded.
type CommitResult struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
}

// ProposeCommit reads the staged changes and has the session's model propose a
// message. Nothing is staged or committed: the person edits the text first.
func (c *Controller) ProposeCommit(ctx context.Context) (CommitProposal, error) {
	snap, err := gitcommit.Staged(ctx, c.workspaceRepo)
	if err != nil {
		return CommitProposal{}, err
	}
	files := make([]commitmsg.File, len(snap.Files))
	for i, f := range snap.Files {
		files[i] = commitmsg.File{Path: f.Path, Status: f.Status}
	}
	workspace := ""
	if root := c.WorkspaceRoot(); root != "" {
		workspace = filepath.Base(root)
	}
	msg, err := c.committer.Generate(ctx, commitmsg.Input{
		Files: files, Diff: snap.Diff, Truncated: snap.Truncated, Recent: snap.Recent, Workspace: workspace,
	})
	if err != nil {
		return CommitProposal{}, err
	}
	return CommitProposal{
		Message: msg, Fingerprint: snap.Fingerprint, Files: snap.Files,
		Truncated: snap.Truncated || len(snap.Diff) > commitmsg.MaxDiffBytes, ContentSecrets: snap.ContentSecrets,
	}, nil
}

// CommitStaged records the confirmed message as one local commit of exactly the
// staged set the person saw.
func (c *Controller) CommitStaged(ctx context.Context, req CommitRequest) (CommitResult, error) {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	res, err := gitcommit.Commit(ctx, c.workspaceRepo, req.Message, req.Fingerprint, req.AcknowledgeSecrets)
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{Hash: res.Hash, Subject: res.Subject}, nil
}
