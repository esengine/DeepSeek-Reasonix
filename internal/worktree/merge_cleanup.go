package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func finalizeCleanupWorktree(ctx context.Context, metadata mergeMetadata, expectedHead string, rootExists bool) ([]MergeBlocker, bool, error) {
	state, hasState, err := readCleanupState(metadata, expectedHead)
	if err != nil {
		return nil, false, err
	}
	if hasState {
		return resumeCleanupState(ctx, metadata, state)
	}
	quarantineRoot, quarantined, err := findQuarantinedWorktree(ctx, metadata, expectedHead)
	if err != nil {
		return nil, false, err
	}
	if quarantined {
		return removeQuarantinedWorktree(ctx, metadata, quarantineRoot, expectedHead)
	}
	if !rootExists {
		registered, registeredErr := worktreeBranchIsRegistered(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
		if registeredErr != nil {
			return nil, false, registeredErr
		}
		if registered {
			return nil, false, errors.New("worktree remains registered at an unexpected path; it was preserved")
		}
		return []MergeBlocker{}, true, nil
	}

	branch, stderr, err := gitValue(ctx, metadata.WorktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		registered, registeredErr := worktreeBranchIsRegistered(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
		if registeredErr != nil {
			return nil, false, registeredErr
		}
		if registered {
			return nil, false, fmt.Errorf("worktree branch remains registered at an unexpected path%s", stderrSuffix(stderr))
		}
		return []MergeBlocker{{
			Code: "late_content_preserved", Message: "content at the former worktree path was preserved", Paths: []string{"."},
		}}, true, nil
	}
	if branch != metadata.WorktreeBranch {
		return nil, false, fmt.Errorf("worktree branch identity changed%s", stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, metadata.WorktreeRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != expectedHead {
		return nil, false, fmt.Errorf("worktree HEAD identity changed%s", stderrSuffix(stderr))
	}
	status, stderr, err := runGitEnv(ctx, metadata.WorktreeRoot, gitNoOptionalLocks, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored")
	if err != nil {
		return nil, false, fmt.Errorf("inspect cleanup safety: %w%s", err, stderrSuffix(stderr))
	}
	paths, err := nulStatusPaths(status)
	if err != nil {
		return nil, false, fmt.Errorf("decode cleanup safety: %w", err)
	}
	if len(paths) > 0 {
		return []MergeBlocker{{Code: "worktree_content", Message: "tracked, untracked, or ignored files block cleanup", Paths: paths}}, false, errors.New("worktree contains files that must be preserved")
	}
	return removeWorktreeThroughQuarantine(ctx, metadata, expectedHead)
}

func removeWorktreeThroughQuarantine(ctx context.Context, metadata mergeMetadata, expectedHead string) ([]MergeBlocker, bool, error) {
	quarantineID, err := randomID()
	if err != nil {
		return nil, false, err
	}
	originalRoot := metadata.WorktreeRoot
	quarantineDir := filepath.Join(filepath.Dir(originalRoot), ".reasonix-cleanup")
	if err := ensureCleanupQuarantineDir(quarantineDir); err != nil {
		return nil, false, err
	}
	quarantineRoot := filepath.Join(quarantineDir, quarantineID)
	if _, err := os.Lstat(quarantineRoot); err == nil {
		return nil, false, errors.New("cleanup quarantine path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("inspect cleanup quarantine path: %w", err)
	}
	if _, stderr, err := runGit(ctx, metadata.SourceRoot, "worktree", "move", originalRoot, quarantineRoot); err != nil {
		return nil, false, fmt.Errorf("quarantine clean worktree: %w%s", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_cleanup_quarantine")
	return removeQuarantinedWorktree(ctx, metadata, quarantineRoot, expectedHead)
}

func removeQuarantinedWorktree(ctx context.Context, metadata mergeMetadata, quarantineRoot, expectedHead string) ([]MergeBlocker, bool, error) {
	originalRoot := metadata.WorktreeRoot
	if err := verifyQuarantinedWorktree(ctx, metadata, quarantineRoot, expectedHead); err != nil {
		restoreErr := restoreQuarantinedWorktree(ctx, metadata.SourceRoot, quarantineRoot, originalRoot)
		if restoreErr != nil {
			return nil, false, fmt.Errorf("cleanup_state_changed; quarantined worktree was preserved at %s: %w", quarantineRoot, errors.Join(err, fmt.Errorf("restore failed: %w", restoreErr)))
		}
		return nil, false, fmt.Errorf("cleanup_state_changed: %w; worktree was restored", err)
	}

	blockers := []MergeBlocker{}
	if _, err := os.Lstat(originalRoot); err == nil {
		blockers = append(blockers, MergeBlocker{
			Code: "late_content_preserved", Message: "content appeared at the former worktree path after quarantine and was preserved", Paths: []string{"."},
		})
	} else if !errors.Is(err, os.ErrNotExist) {
		return blockers, false, fmt.Errorf("inspect former worktree path after quarantine: %w", err)
	}

	noteMergeStep("before_cleanup_quarantine_remove")
	if err := verifyQuarantinedWorktree(ctx, metadata, quarantineRoot, expectedHead); err != nil {
		restoreErr := restoreQuarantinedWorktree(ctx, metadata.SourceRoot, quarantineRoot, originalRoot)
		if restoreErr != nil {
			return blockers, false, fmt.Errorf("cleanup_state_changed; quarantined worktree was preserved at %s: %w", quarantineRoot, errors.Join(err, fmt.Errorf("restore failed: %w", restoreErr)))
		}
		return blockers, false, fmt.Errorf("cleanup_state_changed: %w; worktree was restored", err)
	}
	manifest, err := captureCleanupManifest(ctx, quarantineRoot)
	if err != nil {
		return blockers, false, err
	}
	detachedID, err := randomID()
	if err != nil {
		return blockers, false, err
	}
	state := cleanupState{
		Version: cleanupStateVersion, OriginalRoot: originalRoot,
		RegisteredRoot: quarantineRoot, DetachedRoot: filepath.Join(filepath.Dir(quarantineRoot), "detached-"+detachedID),
		WorktreeBranch: metadata.WorktreeBranch, WorktreeHead: expectedHead,
		Stage: cleanupStagePrepared, Manifest: manifest,
	}
	if _, err := os.Lstat(cleanupJournalPath(metadata)); err == nil {
		return blockers, false, errors.New("cleanup state already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return blockers, false, fmt.Errorf("inspect cleanup state path: %w", err)
	}
	if err := createCleanupState(metadata, state); err != nil {
		return blockers, false, err
	}
	resumedBlockers, removed, err := resumeCleanupState(ctx, metadata, state)
	return append(blockers, resumedBlockers...), removed, err
}

func resumeCleanupState(ctx context.Context, metadata mergeMetadata, state cleanupState) ([]MergeBlocker, bool, error) {
	blockers := []MergeBlocker{}
	if _, err := os.Lstat(state.OriginalRoot); err == nil {
		blockers = append(blockers, MergeBlocker{
			Code: "late_content_preserved", Message: "content appeared at the former worktree path and was preserved", Paths: []string{"."},
		})
	} else if !errors.Is(err, os.ErrNotExist) {
		return blockers, false, fmt.Errorf("inspect former worktree path during cleanup recovery: %w", err)
	}

	if state.Stage == cleanupStagePrepared {
		updated, preparedBlockers, removed, err := advancePreparedCleanup(ctx, metadata, state)
		blockers = append(blockers, preparedBlockers...)
		if err != nil {
			return blockers, removed, err
		}
		state = updated
	}

	if _, err := os.Lstat(state.DetachedRoot); errors.Is(err, os.ErrNotExist) {
		if err := os.Remove(cleanupJournalPath(metadata)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return blockers, true, fmt.Errorf("remove completed cleanup state: %w", err)
		}
		return blockers, true, nil
	} else if err != nil {
		return blockers, true, fmt.Errorf("inspect detached checkout: %w", err)
	}
	detachedBlockers, err := safeRemoveDetachedCheckout(ctx, state)
	blockers = append(blockers, detachedBlockers...)
	if err != nil {
		return blockers, true, err
	}
	if err := os.Remove(cleanupJournalPath(metadata)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return blockers, true, fmt.Errorf("remove completed cleanup state: %w", err)
	}
	_ = os.Remove(filepath.Dir(state.DetachedRoot))
	return blockers, true, nil
}

func advancePreparedCleanup(ctx context.Context, metadata mergeMetadata, state cleanupState) (cleanupState, []MergeBlocker, bool, error) {
	registeredExists, err := cleanupPathExists(state.RegisteredRoot)
	if err != nil {
		return state, nil, false, fmt.Errorf("inspect registered cleanup root: %w", err)
	}
	detachedExists, err := cleanupPathExists(state.DetachedRoot)
	if err != nil {
		return state, nil, false, fmt.Errorf("inspect detached cleanup root: %w", err)
	}
	switch {
	case registeredExists && !detachedExists:
		actual, err := captureCleanupManifest(ctx, state.RegisteredRoot)
		if err != nil || !manifestsEqual(state.Manifest, actual) {
			return state, nil, false, errors.New("cleanup_state_changed: registered quarantine no longer matches its manifest")
		}
		if err := os.Rename(state.RegisteredRoot, state.DetachedRoot); err != nil {
			return state, nil, false, fmt.Errorf("detach quarantined checkout: %w", err)
		}
		noteMergeStep("after_cleanup_detach")
	case !registeredExists && detachedExists:
	case registeredExists && detachedExists:
		return state, nil, false, errors.New("cleanup_state_changed: both registered and detached cleanup roots exist")
	default:
		registered, err := worktreeBranchIsRegistered(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
		if err != nil {
			return state, nil, false, err
		}
		if registered {
			return state, nil, false, errors.New("cleanup_state_changed: cleanup checkout disappeared while still registered")
		}
		state.Stage = cleanupStageUnregistered
		if err := writeCleanupState(metadata, state); err != nil {
			return state, nil, true, err
		}
		return state, nil, true, nil
	}

	actual, err := captureCleanupManifest(ctx, state.DetachedRoot)
	if err != nil || !manifestsEqual(state.Manifest, actual) {
		paths := []string{"."}
		if err == nil {
			paths, _ = unexpectedCleanupPaths(state.Manifest, actual)
		}
		blockers := []MergeBlocker{{Code: "late_content_preserved", Message: "content changed after checkout detachment and was preserved", Paths: paths}}
		return state, blockers, false, errors.New("cleanup_state_changed: detached checkout no longer matches its manifest")
	}
	registered, err := worktreeBranchIsRegistered(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return state, nil, false, err
	}
	if registered {
		if _, stderr, err := runGit(ctx, metadata.SourceRoot, "worktree", "remove", state.RegisteredRoot); err != nil {
			return state, nil, false, fmt.Errorf("unregister detached worktree: %w%s", err, stderrSuffix(stderr))
		}
	}
	registered, err = worktreeBranchIsRegistered(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil || registered {
		return state, nil, false, errors.New("detached worktree registration was not removed")
	}
	state.Stage = cleanupStageUnregistered
	if err := writeCleanupState(metadata, state); err != nil {
		return state, nil, true, err
	}
	return state, nil, true, nil
}

func cleanupPathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func findQuarantinedWorktree(ctx context.Context, metadata mergeMetadata, expectedHead string) (string, bool, error) {
	quarantineDir := filepath.Join(filepath.Dir(metadata.WorktreeRoot), ".reasonix-cleanup")
	entries, err := os.ReadDir(quarantineDir)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect cleanup quarantine: %w", err)
	}
	found := ""
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		candidate := filepath.Join(quarantineDir, entry.Name())
		gitMarker, markerErr := os.Lstat(filepath.Join(candidate, ".git"))
		if errors.Is(markerErr, os.ErrNotExist) {
			continue
		}
		if markerErr != nil {
			return "", false, fmt.Errorf("inspect quarantined checkout marker: %w", markerErr)
		}
		if gitMarker.Mode()&os.ModeSymlink != 0 {
			continue
		}
		matches, matchErr := quarantinedWorktreeIdentityMatches(ctx, metadata, candidate, expectedHead)
		if matchErr != nil {
			return "", false, matchErr
		}
		if !matches {
			continue
		}
		if found != "" {
			return "", false, errors.New("multiple quarantined worktrees match the cleanup identity")
		}
		found = candidate
	}
	return found, found != "", nil
}

func quarantinedWorktreeIdentityMatches(ctx context.Context, metadata mergeMetadata, root, expectedHead string) (bool, error) {
	if err := verifyRepositoryRoot(ctx, root); err != nil {
		return false, fmt.Errorf("inspect quarantined checkout identity: %w", err)
	}
	if err := verifySameCommonDir(ctx, metadata.SourceRoot, root); err != nil {
		return false, nil
	}
	branch, stderr, err := gitValue(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return false, fmt.Errorf("inspect quarantined checkout branch: %w%s", err, stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return false, fmt.Errorf("inspect quarantined checkout HEAD: %w%s", err, stderrSuffix(stderr))
	}
	return branch == metadata.WorktreeBranch && head == expectedHead, nil
}

func worktreeBranchIsRegistered(ctx context.Context, sourceRoot, branch string) (bool, error) {
	out, stderr, err := runGit(ctx, sourceRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("inspect registered worktrees: %w%s", err, stderrSuffix(stderr))
	}
	want := "branch refs/heads/" + branch
	return slices.Contains(strings.Split(out, "\x00"), want), nil
}

func ensureCleanupQuarantineDir(path string) error {
	if err := os.Mkdir(path, 0o700); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create cleanup quarantine: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect cleanup quarantine: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup quarantine is not a real directory")
	}
	return nil
}

func verifyQuarantinedWorktree(ctx context.Context, metadata mergeMetadata, quarantineRoot, expectedHead string) error {
	if err := verifyRepositoryRoot(ctx, quarantineRoot); err != nil {
		return fmt.Errorf("quarantined checkout identity changed: %w", err)
	}
	if err := verifySameCommonDir(ctx, metadata.SourceRoot, quarantineRoot); err != nil {
		return fmt.Errorf("quarantined repository identity changed: %w", err)
	}
	branch, stderr, err := gitValue(ctx, quarantineRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != metadata.WorktreeBranch {
		return fmt.Errorf("quarantined worktree branch changed%s", stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, quarantineRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != expectedHead {
		return fmt.Errorf("quarantined worktree HEAD changed%s", stderrSuffix(stderr))
	}
	status, stderr, err := runGitEnv(ctx, quarantineRoot, gitNoOptionalLocks, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored")
	if err != nil {
		return fmt.Errorf("inspect quarantined cleanup safety: %w%s", err, stderrSuffix(stderr))
	}
	paths, err := nulStatusPaths(status)
	if err != nil {
		return fmt.Errorf("decode quarantined cleanup state: %w", err)
	}
	if len(paths) > 0 {
		return fmt.Errorf("quarantined worktree contains content that must be preserved: %v", paths)
	}
	return nil
}

func restoreQuarantinedWorktree(ctx context.Context, sourceRoot, quarantineRoot, originalRoot string) error {
	if _, err := os.Lstat(quarantineRoot); errors.Is(err, os.ErrNotExist) {
		return errors.New("quarantined worktree no longer exists")
	} else if err != nil {
		return fmt.Errorf("inspect quarantined worktree: %w", err)
	}
	if _, err := os.Lstat(originalRoot); err == nil {
		return errors.New("the original worktree path is occupied")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect original worktree path: %w", err)
	}
	if _, stderr, err := runGit(ctx, sourceRoot, "worktree", "move", quarantineRoot, originalRoot); err != nil {
		return fmt.Errorf("restore quarantined worktree: %w%s", err, stderrSuffix(stderr))
	}
	_ = os.Remove(filepath.Dir(quarantineRoot))
	return nil
}
