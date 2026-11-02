package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// ErrApplyConflict is an ApplyPaths refused because a path it would write was
// changed in the workspace since the snapshot, to something other than the
// candidate's version.
var ErrApplyConflict = errors.New("the workspace changed a path the candidate also changed")

// ErrOutsideWorkspace is a candidate change that lies outside the folder the
// snapshot was taken of.
var ErrOutsideWorkspace = errors.New("the candidate changed a path outside the workspace")

// ConflictError names the paths an ApplyPaths refused over.
type ConflictError struct {
	Paths []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: %s", ErrApplyConflict, strings.Join(e.Paths, ", "))
}

func (e *ConflictError) Unwrap() error { return ErrApplyConflict }

// FileStat is one changed path's line counts; Binary marks a path git could not
// count lines in.
type FileStat struct {
	Status  string
	Path    string
	Added   int
	Removed int
	Binary  bool
}

// writeChanges writes tree's version of each change into repo's files.
func writeChanges(ctx context.Context, repo gitcmd.Repo, tree string, changes []Change) error {
	root, err := os.OpenRoot(repo.WorkTree)
	if err != nil {
		return err
	}
	defer root.Close()
	return writeChangesInRoot(ctx, repo, root, tree, changes)
}

func writeChangesInRoot(ctx context.Context, repo gitcmd.Repo, root *os.Root, tree string, changes []Change) error {
	var deletions []Change
	var restore []string
	for _, ch := range changes {
		if ch.Status == "D" {
			deletions = append(deletions, ch)
		} else {
			restore = append(restore, ch.Path)
		}
	}
	// Restored paths may need directories created by these deletions. Git owns
	// their checkout; deletion must not follow a redirected source ancestor.
	if err := checkApplyAncestors(root, deletions); err != nil {
		return err
	}
	for _, ch := range deletions {
		if err := root.Remove(filepath.FromSlash(ch.Path)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("apply deletion of %s: %w", ch.Path, err)
		}
	}
	if len(restore) == 0 {
		return nil
	}
	slices.Sort(restore)
	args := append([]string{"restore", "--source=" + tree, "--worktree", "--"}, literalPathspecs(restore)...)
	if _, stderr, err := runGit(ctx, repo, args...); err != nil {
		return fmt.Errorf("apply candidate files: %w%s", err, stderrSuffix(stderr))
	}
	return nil
}
