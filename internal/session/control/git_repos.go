package control

import (
	"context"
	"path/filepath"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitrepos"
)

// GitRepos lists the repositories of the workspace: the workspace itself when it
// is one, otherwise the repositories found below it within the search bounds.
func (c *Controller) GitRepos(ctx context.Context) (gitrepos.Result, error) {
	return c.repos.Find(ctx, c.WorkspaceRoot())
}

// RepoFor is the repository a request named by its directory below the workspace,
// as GitRepos lists it. An empty dir is the workspace's own repository, the one
// every git read answered for before a workspace could hold several. A name
// GitRepos would not list is gitrepos.ErrNotListed.
func (c *Controller) RepoFor(ctx context.Context, dir string) (gitcmd.Repo, error) {
	if dir == "" {
		return c.WorkspaceRepo(), nil
	}
	found, err := c.repos.Find(ctx, c.WorkspaceRoot())
	if err != nil {
		return gitcmd.Repo{}, err
	}
	repo, err := found.Find(dir)
	if err != nil {
		return gitcmd.Repo{}, err
	}
	// A repository that was there when the answer was kept can have been replaced
	// in place; its identity is checked against the files before it is used.
	if repo.Verify() != nil {
		fresh, err := gitrepos.Discover(ctx, c.WorkspaceRoot())
		if err != nil {
			return gitcmd.Repo{}, err
		}
		return fresh.Find(dir)
	}
	return repo, nil
}

// repoPrograms is what a bash git read asks the gate about the repository it
// runs in: the program-running keys that repository's own config defines. A
// listing that fails is reported as such rather than read as none.
func (c *Controller) repoPrograms(ctx context.Context, dir string) []string {
	root := c.WorkspaceRoot()
	switch {
	case dir == "":
		dir = root
	case !filepath.IsAbs(dir):
		dir = filepath.Join(root, dir)
	}
	keys, err := gitcmd.RepositoryPrograms(ctx, dir)
	if err != nil {
		return []string{"configuration that could not be listed"}
	}
	return keys
}
