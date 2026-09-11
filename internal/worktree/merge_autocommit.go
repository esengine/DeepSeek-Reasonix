package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/fileutil"
)

var gitNoOptionalLocks = []string{"GIT_OPTIONAL_LOCKS=0"}

// autoCommitDirtyWorktree commits the confirmed filesystem snapshot without
// exposing the user's real index to git add or hooks. The branch ref and index
// are installed through separate compare-and-apply gates; failures after the
// ref CAS are explicitly recovery-required.
func autoCommitDirtyWorktree(ctx context.Context, inspection MergeInspection) (string, bool, error) {
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", false, fmt.Errorf("worktree changed before auto-commit: %w", err)
	}
	indexPath, originalIndex, err := snapshotRealIndex(ctx, inspection.WorktreeRoot)
	if err != nil {
		return "", false, err
	}
	realEntries, stderr, err := runGitEnv(ctx, inspection.WorktreeRoot, gitNoOptionalLocks, "ls-files", "--stage", "-z")
	if err != nil {
		return "", false, fmt.Errorf("snapshot real index entries: %w%s", err, stderrSuffix(stderr))
	}
	stagedIndexChanges, err := hasStagedIndexChanges(ctx, inspection.WorktreeRoot, inspection.WorktreeHead)
	if err != nil {
		return "", false, err
	}

	tempIndex, err := newTemporaryIndex(inspection.WorktreeRoot)
	if err != nil {
		return "", false, err
	}
	defer os.Remove(tempIndex)
	tempEnv := []string{"GIT_OPTIONAL_LOCKS=0", "GIT_INDEX_FILE=" + tempIndex}
	if _, stderr, err := runGitEnv(ctx, inspection.WorktreeRoot, tempEnv, "read-tree", inspection.WorktreeHead); err != nil {
		return "", false, fmt.Errorf("seed temporary index: %w%s", err, stderrSuffix(stderr))
	}
	if err := secureTemporaryIndex(tempIndex); err != nil {
		return "", false, err
	}
	if _, stderr, err := runGitEnv(ctx, inspection.WorktreeRoot, tempEnv, "add", "-A"); err != nil {
		return "", false, fmt.Errorf("stage confirmed changes in temporary index: %w%s", err, stderrSuffix(stderr))
	}
	if err := secureTemporaryIndex(tempIndex); err != nil {
		return "", false, err
	}
	noteMergeStep("after_worktree_add")
	stagedTree, tempEntries, err := verifyTemporaryIndex(ctx, inspection, tempEnv)
	if err != nil {
		return "", false, fmt.Errorf("worktree changed while staging; the real index was preserved: %w", err)
	}
	if stagedIndexChanges && realEntries != tempEntries {
		return "", false, errors.New("worktree_index_split: staged or index-only content is not fully represented by the working tree; commit, stash, or unstage it manually")
	}
	headTree, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "rev-parse", "--verify", inspection.WorktreeHead+"^{tree}")
	if err != nil {
		return "", false, fmt.Errorf("read confirmed worktree tree: %w%s", err, stderrSuffix(stderr))
	}
	if stagedTree == headTree {
		return "", false, errors.New("confirmed worktree snapshot no longer contains committable changes; inspect again")
	}

	committedHead, stderr, err := gitValue(ctx, inspection.WorktreeRoot,
		"-c", "user.name=Reasonix", "-c", "user.email=reasonix@local",
		"commit-tree", stagedTree, "-p", inspection.WorktreeHead, "-m", "worktree: save changes before merge back")
	if err != nil {
		return "", false, fmt.Errorf("create exact worktree commit: %w%s", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_worktree_commit")
	if err := verifyCommitObject(ctx, inspection.WorktreeRoot, committedHead, inspection.WorktreeHead, stagedTree); err != nil {
		return "", false, fmt.Errorf("auto-commit object identity changed: %w", err)
	}
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", false, fmt.Errorf("worktree changed before auto-commit ref update: %w", err)
	}
	noteMergeStep("before_worktree_ref_transaction")
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", false, fmt.Errorf("worktree changed at auto-commit ref update: %w", err)
	}
	branchRef := "refs/heads/" + inspection.WorktreeBranch
	input := fmt.Sprintf("update %s %s %s\n", branchRef, committedHead, inspection.WorktreeHead)
	if _, stderr, err := runGitInput(ctx, inspection.WorktreeRoot, input, "update-ref", "--stdin"); err != nil {
		installed, verifyErr := refEquals(ctx, inspection.WorktreeRoot, branchRef, committedHead)
		if verifyErr != nil || installed {
			return "", true, fmt.Errorf("auto-commit ref transaction is uncertain; recovery is required: %w%s", err, stderrSuffix(stderr))
		}
		return "", false, fmt.Errorf("worktree branch changed before auto-commit ref update; the real index was preserved: %w%s", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_worktree_ref_update")
	if err := verifyWorktreeCheckout(ctx, inspection, committedHead); err != nil {
		return "", true, fmt.Errorf("auto-commit ref was installed but checkout identity changed; recovery is required: %w", err)
	}
	if err := verifyTemporaryIndexAgainstWorktree(ctx, inspection.WorktreeRoot, tempEnv, stagedTree); err != nil {
		return "", true, fmt.Errorf("auto-commit ref was installed but worktree contents changed; recovery is required: %w", err)
	}
	noteMergeStep("before_worktree_index_sync")
	if err := installIndexFileCAS(indexPath, tempIndex, originalIndex); err != nil {
		return "", true, fmt.Errorf("auto-commit ref was installed but the real index changed; recovery is required: %w", err)
	}
	noteMergeStep("after_worktree_index_sync")
	if err := verifyAutoCommitSuccess(ctx, inspection, committedHead, stagedTree); err != nil {
		return "", true, fmt.Errorf("auto-commit was installed but final verification failed; recovery is required: %w", err)
	}
	return committedHead, false, nil
}

func newTemporaryIndex(worktreeRoot string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(worktreeRoot), ".reasonix-merge-index-*")
	if err != nil {
		return "", fmt.Errorf("allocate temporary index: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temporary index placeholder: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("prepare temporary index path: %w", err)
	}
	return path, nil
}

func secureTemporaryIndex(path string) error {
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure temporary index: %w", err)
	}
	return nil
}

func snapshotRealIndex(ctx context.Context, root string) (string, []byte, error) {
	indexPath, stderr, err := gitValue(ctx, root, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", nil, fmt.Errorf("resolve real index path: %w%s", err, stderrSuffix(stderr))
	}
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(root, indexPath)
	}
	body, err := os.ReadFile(indexPath)
	if err != nil {
		return "", nil, fmt.Errorf("snapshot real index: %w", err)
	}
	return filepath.Clean(indexPath), body, nil
}

func verifyTemporaryIndex(ctx context.Context, inspection MergeInspection, env []string) (string, string, error) {
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", "", err
	}
	tree, stderr, err := gitValueEnv(ctx, inspection.WorktreeRoot, env, "write-tree")
	if err != nil {
		return "", "", fmt.Errorf("record temporary index tree: %w%s", err, stderrSuffix(stderr))
	}
	if err := verifyTemporaryIndexAgainstWorktree(ctx, inspection.WorktreeRoot, env, tree); err != nil {
		return "", "", err
	}
	entries, stderr, err := runGitEnv(ctx, inspection.WorktreeRoot, env, "ls-files", "--stage", "-z")
	if err != nil {
		return "", "", fmt.Errorf("snapshot temporary index entries: %w%s", err, stderrSuffix(stderr))
	}
	return tree, entries, nil
}

func verifyTemporaryIndexAgainstWorktree(ctx context.Context, root string, env []string, expectedTree string) error {
	if _, stderr, err := runGitEnv(ctx, root, env, "diff", "--quiet", "--"); err != nil {
		if exitCode(err) == 1 {
			return errors.New("working tree differs from the temporary index")
		}
		return fmt.Errorf("verify temporary index worktree: %w%s", err, stderrSuffix(stderr))
	}
	untracked, stderr, err := runGitEnv(ctx, root, env, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("inspect untracked files with temporary index: %w%s", err, stderrSuffix(stderr))
	}
	if untracked != "" {
		return errors.New("untracked files remain outside the temporary index")
	}
	tree, stderr, err := gitValueEnv(ctx, root, env, "write-tree")
	if err != nil {
		return fmt.Errorf("re-read temporary index tree: %w%s", err, stderrSuffix(stderr))
	}
	if tree != expectedTree {
		return errors.New("temporary index tree changed while verifying the worktree")
	}
	return nil
}

func installIndexFileCAS(indexPath, preparedPath string, expected []byte) (err error) {
	info, err := os.Stat(indexPath)
	if err != nil {
		return fmt.Errorf("inspect real index: %w", err)
	}
	lockPath := indexPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("lock real index: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = lock.Close()
			_ = os.Remove(lockPath)
		}
	}()
	current, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("re-read real index: %w", err)
	}
	if !bytes.Equal(current, expected) {
		return errors.New("real index bytes no longer match the confirmed snapshot")
	}
	prepared, err := os.Open(preparedPath)
	if err != nil {
		return fmt.Errorf("open prepared index: %w", err)
	}
	_, copyErr := io.Copy(lock, prepared)
	closePreparedErr := prepared.Close()
	if copyErr != nil {
		return fmt.Errorf("copy prepared index: %w", copyErr)
	}
	if closePreparedErr != nil {
		return fmt.Errorf("close prepared index: %w", closePreparedErr)
	}
	if err := lock.Sync(); err != nil {
		return fmt.Errorf("sync prepared index: %w", err)
	}
	if err := lock.Close(); err != nil {
		return fmt.Errorf("close prepared index: %w", err)
	}
	if err := fileutil.ClaimRename(lockPath, indexPath); err != nil {
		return fmt.Errorf("install prepared index: %w", err)
	}
	committed = true
	return nil
}

func verifyCommitObject(ctx context.Context, root, commit, expectedParent, expectedTree string) error {
	line, stderr, err := gitValue(ctx, root, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return fmt.Errorf("read auto-commit parents: %w%s", err, stderrSuffix(stderr))
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != commit || fields[1] != expectedParent {
		return errors.New("auto-commit does not have the confirmed HEAD as its unique parent")
	}
	tree, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return fmt.Errorf("read auto-commit tree: %w%s", err, stderrSuffix(stderr))
	}
	if tree != expectedTree {
		return errors.New("auto-commit tree differs from the confirmed temporary index tree")
	}
	return nil
}

func verifyWorktreeCheckout(ctx context.Context, inspection MergeInspection, expectedHead string) error {
	branch, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != inspection.WorktreeBranch {
		return fmt.Errorf("worktree branch is %q, expected %q%s", branch, inspection.WorktreeBranch, stderrSuffix(stderr))
	}
	branchHead, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "rev-parse", "--verify", "refs/heads/"+inspection.WorktreeBranch)
	if err != nil || branchHead != expectedHead {
		return fmt.Errorf("worktree branch HEAD is %s, expected %s%s", branchHead, expectedHead, stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != expectedHead {
		return fmt.Errorf("worktree HEAD is %s, expected %s%s", head, expectedHead, stderrSuffix(stderr))
	}
	operation, err := gitOperation(ctx, inspection.WorktreeRoot)
	if err != nil {
		return err
	}
	if operation != "" {
		return fmt.Errorf("worktree Git %s operation is in progress", operation)
	}
	return nil
}

func verifyAutoCommitSuccess(ctx context.Context, inspection MergeInspection, committedHead, expectedTree string) error {
	if err := verifyWorktreeCheckout(ctx, inspection, committedHead); err != nil {
		return err
	}
	if err := verifyCommitObject(ctx, inspection.WorktreeRoot, committedHead, inspection.WorktreeHead, expectedTree); err != nil {
		return err
	}
	status, stderr, err := runGitEnv(ctx, inspection.WorktreeRoot, gitNoOptionalLocks, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("verify auto-commit status: %w%s", err, stderrSuffix(stderr))
	}
	if strings.TrimSpace(status) != "" {
		return errors.New("worktree is not clean after auto-commit")
	}
	return nil
}

func gitValueEnv(ctx context.Context, root string, env []string, args ...string) (string, string, error) {
	out, stderr, err := runGitEnv(ctx, root, env, args...)
	return strings.TrimSpace(out), stderr, err
}

func hasStagedIndexChanges(ctx context.Context, root, head string) (bool, error) {
	_, stderr, err := runGitEnv(ctx, root, gitNoOptionalLocks, "diff", "--cached", "--quiet", head, "--")
	if err == nil {
		return false, nil
	}
	if exitCode(err) == 1 {
		return true, nil
	}
	return false, fmt.Errorf("inspect staged index changes: %w%s", err, stderrSuffix(stderr))
}

func refEquals(ctx context.Context, root, ref, expected string) (bool, error) {
	value, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", ref)
	if err != nil {
		return false, fmt.Errorf("read ref %s: %w%s", ref, err, stderrSuffix(stderr))
	}
	return value == expected, nil
}
