package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type cleanupRetention struct {
	Blockers                   []MergeBlocker
	RecoveryRoot               string
	RecoveryRetained           bool
	RecoveryWorktreeRegistered bool
	BranchRetained             bool
	LegacyCompleted            bool
}

type registeredWorktree struct {
	Root   string
	Head   string
	Branch string
}

func emptyCleanupRetention() cleanupRetention {
	return cleanupRetention{Blockers: []MergeBlocker{}}
}

func finalizeCleanupWorktree(ctx context.Context, metadata mergeMetadata, expectedHead string, rootExists bool) (cleanupRetention, error) {
	journal, hasState, err := readCleanupState(metadata, expectedHead)
	if err != nil {
		return emptyCleanupRetention(), err
	}
	if hasState {
		if journal.Current != nil {
			return resumeRetainedCleanup(ctx, metadata, *journal.Current)
		}
		return migrateLegacyCleanup(ctx, metadata, *journal.Legacy)
	}

	entries, err := registeredWorktreesForBranch(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return emptyCleanupRetention(), err
	}
	if len(entries) > 1 {
		return emptyCleanupRetention(), errors.New("multiple registered worktrees use the cleanup branch; all were preserved")
	}
	if len(entries) == 1 && !sameCleanupPath(entries[0].Root, metadata.WorktreeRoot) {
		if entries[0].Head != expectedHead || validateCleanupRecoveryPath(metadata, entries[0].Root) != nil {
			return emptyCleanupRetention(), errors.New("worktree remains registered at an unexpected path; it was preserved")
		}
		state := cleanupState{
			Version: cleanupStateVersion, OriginalRoot: metadata.WorktreeRoot, RecoveryRoot: entries[0].Root,
			WorktreeBranch: metadata.WorktreeBranch, WorktreeHead: expectedHead, Stage: cleanupStageRetained,
		}
		if err := createCleanupState(metadata, state); err != nil {
			return emptyCleanupRetention(), err
		}
		return resumeRetainedCleanup(ctx, metadata, state)
	}
	if !rootExists {
		branchHead, branchExists, branchErr := cleanupBranchHead(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
		if branchErr != nil {
			return emptyCleanupRetention(), branchErr
		}
		if len(entries) == 0 && !branchExists {
			retention := emptyCleanupRetention()
			retention.LegacyCompleted = true
			return retention, nil
		}
		retention := emptyCleanupRetention()
		retention.BranchRetained = branchExists
		if branchExists && branchHead != expectedHead {
			return retention, errors.New("recovery_required: temporary branch changed after merge and was preserved")
		}
		return retention, errors.New("recovery_required: the registered recovery checkout could not be located")
	}
	if len(entries) != 1 || !sameCleanupPath(entries[0].Root, metadata.WorktreeRoot) {
		return emptyCleanupRetention(), errors.New("worktree registration does not match the managed checkout; resources were preserved")
	}
	return beginRetainedCleanup(ctx, metadata, expectedHead)
}

func beginRetainedCleanup(ctx context.Context, metadata mergeMetadata, expectedHead string) (cleanupRetention, error) {
	paths, err := verifyCleanupWorktree(ctx, metadata, metadata.WorktreeRoot, expectedHead)
	if err != nil {
		retention := emptyCleanupRetention()
		if len(paths) > 0 {
			retention.Blockers = append(retention.Blockers, MergeBlocker{
				Code: "worktree_content", Message: "tracked, untracked, or ignored files block finalization", Paths: paths,
			})
		}
		return retention, err
	}
	cleanupDir := filepath.Join(filepath.Dir(metadata.WorktreeRoot), ".reasonix-cleanup")
	if err := ensureCleanupRecoveryDir(cleanupDir); err != nil {
		return emptyCleanupRetention(), err
	}
	recoveryID, err := randomID()
	if err != nil {
		return emptyCleanupRetention(), err
	}
	state := cleanupState{
		Version: cleanupStateVersion, OriginalRoot: metadata.WorktreeRoot,
		RecoveryRoot:   filepath.Join(cleanupDir, "recovery-"+recoveryID),
		WorktreeBranch: metadata.WorktreeBranch, WorktreeHead: expectedHead, Stage: cleanupStagePlanned,
	}
	if _, err := os.Lstat(state.RecoveryRoot); err == nil {
		return emptyCleanupRetention(), errors.New("cleanup recovery path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return emptyCleanupRetention(), fmt.Errorf("inspect cleanup recovery path: %w", err)
	}
	if err := createCleanupState(metadata, state); err != nil {
		return emptyCleanupRetention(), err
	}
	return resumeRetainedCleanup(ctx, metadata, state)
}

func resumeRetainedCleanup(ctx context.Context, metadata mergeMetadata, state cleanupState) (cleanupRetention, error) {
	retention := emptyCleanupRetention()
	retention.RecoveryRoot = state.RecoveryRoot
	branchHead, branchExists, err := cleanupBranchHead(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return retention, err
	}
	retention.BranchRetained = branchExists
	if !branchExists || branchHead != state.WorktreeHead {
		return retention, errors.New("recovery_required: temporary branch identity changed; the checkout was preserved")
	}
	entries, err := registeredWorktreesForBranch(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return retention, err
	}
	if len(entries) != 1 || entries[0].Head != state.WorktreeHead {
		return retention, errors.New("recovery_required: recovery worktree registration changed; all resources were preserved")
	}

	registeredRoot := entries[0].Root
	if state.Stage == cleanupStagePlanned && sameCleanupPath(registeredRoot, state.OriginalRoot) {
		if _, err := os.Lstat(state.RecoveryRoot); err == nil {
			return retention, errors.New("cleanup_state_changed: planned recovery path is occupied; both paths were preserved")
		} else if !errors.Is(err, os.ErrNotExist) {
			return retention, fmt.Errorf("inspect planned recovery path: %w", err)
		}
		paths, verifyErr := verifyCleanupWorktree(ctx, metadata, state.OriginalRoot, state.WorktreeHead)
		if verifyErr != nil {
			if len(paths) > 0 {
				retention.Blockers = append(retention.Blockers, MergeBlocker{
					Code: "worktree_content", Message: "tracked, untracked, or ignored files block finalization", Paths: paths,
				})
			}
			return retention, verifyErr
		}
		noteMergeStep("before_cleanup_recovery_move")
		if _, stderr, moveErr := runGit(ctx, metadata.SourceRoot, "worktree", "move", state.OriginalRoot, state.RecoveryRoot); moveErr != nil {
			return retention, fmt.Errorf("move worktree to retained recovery path: %w%s", moveErr, stderrSuffix(stderr))
		}
		noteMergeStep("after_cleanup_recovery_move")
		entries, err = registeredWorktreesForBranch(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
		if err != nil {
			return retention, err
		}
		if len(entries) != 1 {
			return retention, errors.New("recovery_required: worktree registration changed after recovery move")
		}
		registeredRoot = entries[0].Root
	}
	if !sameCleanupPath(registeredRoot, state.RecoveryRoot) {
		return retention, errors.New("recovery_required: registered recovery path differs from the cleanup journal")
	}
	retention.RecoveryWorktreeRegistered = true
	retention.RecoveryRetained = true

	paths, verifyErr := verifyCleanupWorktree(ctx, metadata, state.RecoveryRoot, state.WorktreeHead)
	if verifyErr != nil {
		if len(paths) > 0 {
			retention.Blockers = append(retention.Blockers, MergeBlocker{
				Code: "late_content_preserved", Message: "content changed in the retained recovery worktree and was preserved", Paths: paths,
			})
		}
		return retention, fmt.Errorf("cleanup_state_changed: %w", verifyErr)
	}
	if state.Stage != cleanupStageRetained {
		state.Stage = cleanupStageRetained
		if err := writeCleanupState(metadata, state); err != nil {
			return retention, err
		}
	}
	if _, err := os.Lstat(state.OriginalRoot); err == nil {
		retention.Blockers = append(retention.Blockers, MergeBlocker{
			Code: "late_content_preserved", Message: "content appeared at the former worktree path and was preserved", Paths: []string{"."},
		})
	} else if !errors.Is(err, os.ErrNotExist) {
		return retention, fmt.Errorf("inspect former worktree path after recovery move: %w", err)
	}
	return retention, nil
}

func migrateLegacyCleanup(ctx context.Context, metadata mergeMetadata, legacy legacyCleanupState) (cleanupRetention, error) {
	retention := emptyCleanupRetention()
	branchHead, branchExists, err := cleanupBranchHead(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return retention, err
	}
	retention.BranchRetained = branchExists
	registeredExists, err := cleanupPathExists(legacy.RegisteredRoot)
	if err != nil {
		return retention, fmt.Errorf("inspect legacy registered cleanup root: %w", err)
	}
	detachedExists, err := cleanupPathExists(legacy.DetachedRoot)
	if err != nil {
		return retention, fmt.Errorf("inspect legacy detached cleanup root: %w", err)
	}
	if registeredExists && detachedExists {
		return retention, errors.New("recovery_required: both legacy cleanup roots exist; both were preserved")
	}
	entries, err := registeredWorktreesForBranch(ctx, metadata.SourceRoot, metadata.WorktreeBranch)
	if err != nil {
		return retention, err
	}
	if len(entries) > 1 {
		return retention, errors.New("recovery_required: multiple worktrees use the legacy cleanup branch")
	}
	if len(entries) == 1 && entries[0].Head == legacy.WorktreeHead && sameCleanupPath(entries[0].Root, legacy.RegisteredRoot) {
		switch {
		case registeredExists:
			if err := verifyLegacyManifest(ctx, legacy.RegisteredRoot, legacy.Manifest); err != nil {
				return retention, err
			}
		case detachedExists:
			if err := verifyLegacyManifest(ctx, legacy.DetachedRoot, legacy.Manifest); err != nil {
				return retention, err
			}
			if err := os.Rename(legacy.DetachedRoot, legacy.RegisteredRoot); err != nil {
				return retention, fmt.Errorf("restore registered legacy recovery worktree: %w", err)
			}
		default:
			return retention, errors.New("recovery_required: legacy recovery checkout disappeared while still registered")
		}
		state := cleanupState{
			Version: cleanupStateVersion, OriginalRoot: legacy.OriginalRoot, RecoveryRoot: legacy.RegisteredRoot,
			WorktreeBranch: legacy.WorktreeBranch, WorktreeHead: legacy.WorktreeHead, Stage: cleanupStageRetained,
		}
		if err := writeCleanupState(metadata, state); err != nil {
			return retention, err
		}
		return resumeRetainedCleanup(ctx, metadata, state)
	}
	if detachedExists || registeredExists {
		retention.RecoveryRoot = legacy.DetachedRoot
		if registeredExists {
			retention.RecoveryRoot = legacy.RegisteredRoot
		}
		retention.Blockers = append(retention.Blockers, MergeBlocker{
			Code: "legacy_recovery_preserved", Message: "a legacy detached recovery checkout was preserved for manual repair", Paths: []string{"."},
		})
		return retention, errors.New("recovery_required: legacy cleanup state is no longer a registered worktree")
	}
	if len(entries) == 0 && !branchExists {
		retention.LegacyCompleted = true
		return retention, nil
	}
	if branchExists && branchHead != legacy.WorktreeHead {
		return retention, errors.New("recovery_required: legacy recovery branch changed and was preserved")
	}
	return retention, errors.New("recovery_required: legacy cleanup identity is incomplete; remaining resources were preserved")
}

func verifyLegacyManifest(ctx context.Context, root string, expected []cleanupManifestEntry) error {
	actual, err := captureCleanupManifest(ctx, root)
	if err != nil {
		return err
	}
	if !manifestsEqual(expected, actual) {
		return errors.New("cleanup_state_changed: legacy recovery checkout no longer matches its manifest")
	}
	return nil
}

func verifyCleanupWorktree(ctx context.Context, metadata mergeMetadata, root, expectedHead string) ([]string, error) {
	if err := verifyRepositoryRoot(ctx, root); err != nil {
		return nil, fmt.Errorf("recovery checkout identity changed: %w", err)
	}
	if err := verifySameCommonDir(ctx, metadata.SourceRoot, root); err != nil {
		return nil, fmt.Errorf("recovery repository identity changed: %w", err)
	}
	branch, stderr, err := gitValue(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != metadata.WorktreeBranch {
		return nil, fmt.Errorf("recovery worktree branch changed%s", stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil || head != expectedHead {
		return nil, fmt.Errorf("recovery worktree HEAD changed%s", stderrSuffix(stderr))
	}
	operation, err := gitOperation(ctx, root)
	if err != nil {
		return nil, err
	}
	if operation != "" {
		return nil, fmt.Errorf("recovery worktree has an active Git %s operation", operation)
	}
	status, stderr, err := runGitEnv(ctx, root, gitNoOptionalLocks, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored")
	if err != nil {
		return nil, fmt.Errorf("inspect recovery worktree state: %w%s", err, stderrSuffix(stderr))
	}
	paths, err := nulStatusPaths(status)
	if err != nil {
		return nil, fmt.Errorf("decode recovery worktree state: %w", err)
	}
	if len(paths) > 0 {
		return paths, errors.New("recovery worktree contains content that must be preserved")
	}
	return []string{}, nil
}

func registeredWorktreesForBranch(ctx context.Context, sourceRoot, branch string) ([]registeredWorktree, error) {
	out, stderr, err := runGit(ctx, sourceRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("inspect registered worktrees: %w%s", err, stderrSuffix(stderr))
	}
	want := "refs/heads/" + branch
	entries := []registeredWorktree{}
	current := registeredWorktree{}
	appendCurrent := func() {
		if current.Root != "" && current.Branch == want {
			entries = append(entries, current)
		}
		current = registeredWorktree{}
	}
	for record := range strings.SplitSeq(out, "\x00") {
		switch {
		case strings.HasPrefix(record, "worktree "):
			appendCurrent()
			current.Root = strings.TrimPrefix(record, "worktree ")
		case strings.HasPrefix(record, "HEAD "):
			current.Head = strings.TrimPrefix(record, "HEAD ")
		case strings.HasPrefix(record, "branch "):
			current.Branch = strings.TrimPrefix(record, "branch ")
		}
	}
	appendCurrent()
	return entries, nil
}

func cleanupBranchHead(ctx context.Context, sourceRoot, branch string) (string, bool, error) {
	head, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "refs/heads/"+branch)
	if err == nil {
		return head, true, nil
	}
	if exitCode(err) == 128 || exitCode(err) == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("inspect temporary branch: %w%s", err, stderrSuffix(stderr))
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

func sameCleanupPath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	leftInfo, leftStatErr := os.Stat(leftAbs)
	rightInfo, rightStatErr := os.Stat(rightAbs)
	if leftStatErr == nil && rightStatErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	if resolvedLeft, err := resolveMissingCleanupPath(leftAbs); err == nil {
		leftAbs = resolvedLeft
	}
	if resolvedRight, err := resolveMissingCleanupPath(rightAbs); err == nil {
		rightAbs = resolvedRight
	}
	leftAbs, rightAbs = filepath.Clean(leftAbs), filepath.Clean(rightAbs)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftAbs, rightAbs)
	}
	return leftAbs == rightAbs
}

func ensureCleanupRecoveryDir(path string) error {
	if err := os.Mkdir(path, 0o700); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create cleanup recovery directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect cleanup recovery directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup recovery directory is not a real directory")
	}
	return nil
}
