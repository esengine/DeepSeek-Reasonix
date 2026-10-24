package control

import (
	"bytes"
	"context"
	"path/filepath"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/state/mutation"
)

// DiscardRequest names one changed file whose change is to be thrown away.
type DiscardRequest struct {
	// Repo names the repository below the workspace; empty is the workspace's own.
	Repo string `json:"repo,omitempty"`
	Path string `json:"path"`
}

// DiscardChange puts one modified or deleted file back as the last commit has it.
// The change is gone afterwards and nothing keeps a copy, so it is refused while a
// turn runs, which may be writing the file, as a branch switch is.
func (c *Controller) DiscardChange(ctx context.Context, req DiscardRequest) error {
	repo, err := c.RepoFor(ctx, req.Repo)
	if err != nil {
		return err
	}
	version, err := c.discardVersion(ctx, repo, req.Path)
	if err != nil {
		return err
	}
	c.commitMu.Lock()
	defer c.commitMu.Unlock()
	c.mu.Lock()
	busy := c.gate.busy()
	c.mu.Unlock()
	if busy {
		return ErrTurnRunning
	}
	activity, err := c.holdWorkspaceActivity(ctx)
	if err != nil {
		return err
	}
	defer activity.release()
	lease, err := c.holdGitMutationExpected(ctx, repo, "git.discard", false, version.files, req.Path)
	if err != nil {
		return err
	}
	defer lease.End(mutation.Unknown)
	if c.mutations != nil {
		head, err := repo.Command(ctx, "rev-parse", "--verify", "HEAD").Output()
		if err != nil {
			return err
		}
		if !bytes.Equal(head, version.head) {
			return &mutation.VersionConflict{Path: filepath.Join(repo.GitDir, "HEAD")}
		}
	}
	return gitcommit.Discard(ctx, repo, req.Path)
}

type discardVersion struct {
	head  []byte
	files map[string]mutation.ContentVersion
}

func (c *Controller) discardVersion(ctx context.Context, repo gitcmd.Repo, path string) (discardVersion, error) {
	if c.mutations == nil {
		return discardVersion{}, nil
	}
	if !filepath.IsLocal(path) {
		return discardVersion{}, gitcommit.ErrUnknownPath
	}
	head, err := repo.Command(ctx, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		return discardVersion{}, err
	}
	version := discardVersion{head: head, files: map[string]mutation.ContentVersion{}}
	for _, input := range []string{filepath.Join(repo.Dir, filepath.FromSlash(path)), filepath.Join(repo.GitDir, "index")} {
		v, err := mutation.ReadVersion(input)
		if err != nil {
			return discardVersion{}, err
		}
		version.files[input] = v
	}
	return version, nil
}
