package worktree

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// An extra parent anchors the metadata blob through the candidate HEAD.
// Git GC then retains it together with the base without changing source refs
// or adding a metadata file to the checked-out source tree.
func snapshotMetadataAnchor(ctx context.Context, repo gitcmd.Repo, blob string) (string, error) {
	tree, stderr, err := runGitInput(ctx, repo, "100644 blob "+blob+"\tfilesystem-modes-v1.json\n", "mktree")
	if err != nil {
		return "", fmt.Errorf("anchor snapshot metadata tree: %w%s", err, stderrSuffix(stderr))
	}
	commit, stderr, err := runGitEnv(ctx, repo, snapshotIdentity, "commit-tree", strings.TrimSpace(tree), "-m", "reasonix snapshot filesystem modes")
	if err != nil {
		return "", fmt.Errorf("anchor snapshot metadata commit: %w%s", err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(commit), nil
}
