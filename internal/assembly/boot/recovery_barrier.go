package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func mutationRepositories(ctx context.Context, repo gitcmd.Repo, extras []string) ([]gitcmd.Repo, []string, error) {
	var repos []gitcmd.Repo
	var resources []string
	if repo.Absent != "" {
		return nil, nil, fmt.Errorf("%w: cannot identify workspace Git metadata", mutation.ErrInvalidIntent)
	}
	if repo.Valid() {
		repos = append(repos, repo)
		resources = append(resources, repo.GitDir, repo.CommonDir)
	}
	for _, root := range extras {
		directory, err := writeRootDiscoveryDirectory(root)
		if err != nil {
			return nil, nil, err
		}
		extra := workspaceRepo(ctx, gitcmd.Repo{}, directory)
		if extra.Absent != "" {
			return nil, nil, fmt.Errorf("%w: cannot identify Git metadata for %s", mutation.ErrInvalidIntent, root)
		}
		resources = append(resources, root)
		if extra.Valid() {
			duplicate := false
			for _, known := range repos {
				if known.WorkTree == extra.WorkTree && known.GitDir == extra.GitDir && known.CommonDir == extra.CommonDir {
					duplicate = true
					break
				}
			}
			if !duplicate {
				repos = append(repos, extra)
			}
			resources = append(resources, extra.GitDir, extra.CommonDir)
		}
	}
	return repos, resources, nil
}

func writeRootDiscoveryDirectory(root string) (string, error) {
	path, err := fileutil.CanonicalWritePath(root)
	if err != nil {
		return "", err
	}
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", mutation.ErrInvalidIntent
		}
		path = parent
	}
}

func repositoryAffected(repo gitcmd.Repo, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, path := range paths {
		for _, root := range []string{repo.WorkTree, repo.GitDir, repo.CommonDir} {
			if fileutil.AtOrUnder(path, root) || fileutil.AtOrUnder(root, path) {
				return true
			}
		}
	}
	return false
}

func recoveryBarrier(repos []gitcmd.Repo, paths []string) error {
	for _, repo := range repos {
		if !repositoryAffected(repo, paths) {
			continue
		}
		if err := worktree.CheckApplyWriteBarrier(repo); err != nil {
			return tool.Refusal{Code: "isolation.recovery_required", Message: err.Error() + "; reopen with the shared coordination strategy to recover before changing write strategy"}
		}
	}
	return nil
}

func repositoriesAdmission(ctx context.Context, repos []gitcmd.Repo, intent mutation.Intent) error {
	if intent.Effect == mutation.CapacityOnly {
		return nil
	}
	paths := intent.Paths
	if intent.Effect == mutation.Workspace {
		paths = nil
	}
	for _, repo := range repos {
		if !repositoryAffected(repo, paths) {
			continue
		}
		if intent.Effect == mutation.Workspace || intent.Effect == mutation.GitMetadata {
			if err := repo.VerifyDiscovery(ctx); err != nil {
				return err
			}
		} else if err := repo.Verify(); err != nil {
			return err
		}
		if err := integrationAdmission(repo)(intent); err != nil {
			return err
		}
	}
	return nil
}
