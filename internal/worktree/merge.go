package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MergeBlocker is a stable, structured reason why merge or cleanup cannot
// proceed. Message remains suitable for older clients while Code lets newer
// clients localize and group the failure.
type MergeBlocker struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

// MergeInspection describes the exact identities used by a later merge
// request. Every mutable identity must be sent back by the caller.
type MergeInspection struct {
	Available          bool           `json:"available"`
	Reason             string         `json:"reason,omitempty"`
	CanMerge           bool           `json:"canMerge"`
	AlreadyMerged      bool           `json:"alreadyMerged"`
	WorktreeRoot       string         `json:"worktreeRoot,omitempty"`
	SourceRoot         string         `json:"sourceRoot,omitempty"`
	WorktreeBranch     string         `json:"worktreeBranch,omitempty"`
	TargetBranch       string         `json:"targetBranch,omitempty"`
	CreatedHead        string         `json:"createdHead,omitempty"`
	WorktreeHead       string         `json:"worktreeHead,omitempty"`
	WorktreeStateToken string         `json:"worktreeStateToken,omitempty"`
	TargetHead         string         `json:"targetHead,omitempty"`
	AheadCount         int            `json:"aheadCount"`
	BehindCount        int            `json:"behindCount"`
	FilesChanged       int            `json:"filesChanged"`
	Insertions         int            `json:"insertions"`
	Deletions          int            `json:"deletions"`
	ChangedFiles       []string       `json:"changedFiles"`
	HasConflicts       bool           `json:"hasConflicts"`
	ConflictFiles      []string       `json:"conflictFiles"`
	WorktreeDirty      bool           `json:"worktreeDirty"`
	SourceDirty        bool           `json:"sourceDirty"`
	Blockers           []MergeBlocker `json:"blockers"`
	CleanupBlockers    []MergeBlocker `json:"cleanupBlockers"`
}

// MergeRequest proves that the user confirmed a specific inspection. A target
// branch or HEAD drift never silently turns into a different merge.
type MergeRequest struct {
	WorkspaceRoot              string `json:"workspaceRoot"`
	ExpectedTargetBranch       string `json:"expectedTargetBranch"`
	ExpectedTargetHead         string `json:"expectedTargetHead"`
	ExpectedWorktreeHead       string `json:"expectedWorktreeHead"`
	ExpectedWorktreeStateToken string `json:"expectedWorktreeStateToken"`
	AutoCommitDirty            bool   `json:"autoCommitDirty"`
}

// MergeResult is a merge receipt and cleanup identity. MergeBack never removes
// the worktree or its temporary branch.
type MergeResult struct {
	Merged           bool   `json:"merged"`
	AlreadyMerged    bool   `json:"alreadyMerged"`
	RecoveryRequired bool   `json:"recoveryRequired"`
	SourceRoot       string `json:"sourceRoot,omitempty"`
	TargetBranch     string `json:"targetBranch,omitempty"`
	TargetHead       string `json:"targetHead,omitempty"`
	MergedCommit     string `json:"mergedCommit,omitempty"`
	WorktreeRoot     string `json:"worktreeRoot,omitempty"`
	WorktreeBranch   string `json:"worktreeBranch,omitempty"`
	WorktreeHead     string `json:"worktreeHead,omitempty"`
	Error            string `json:"error,omitempty"`
}

// CleanupRequest carries the immutable merge receipt needed for a safe retry.
type CleanupRequest struct {
	WorktreeRoot   string `json:"worktreeRoot"`
	SourceRoot     string `json:"sourceRoot"`
	TargetBranch   string `json:"targetBranch"`
	MergedCommit   string `json:"mergedCommit"`
	WorktreeBranch string `json:"worktreeBranch"`
	WorktreeHead   string `json:"worktreeHead"`
}

// CleanupResult reports partial success without hiding recoverable resources.
type CleanupResult struct {
	Completed       bool           `json:"completed"`
	WorktreeRemoved bool           `json:"worktreeRemoved"`
	BranchDeleted   bool           `json:"branchDeleted"`
	Blockers        []MergeBlocker `json:"blockers"`
	Error           string         `json:"error,omitempty"`
}

// mergeStepHook is test-only. Tests install it before starting a merge and do
// not mutate it concurrently; it makes otherwise sub-millisecond identity
// windows deterministic without weakening production checks.
var mergeStepHook func(string)

func noteMergeStep(step string) {
	if mergeStepHook != nil {
		mergeStepHook(step)
	}
}

// InspectMerge performs a failure-closed inspection using creation metadata.
func InspectMerge(ctx context.Context, workspaceRoot, managedRoot string) (MergeInspection, error) {
	inspection := emptyMergeInspection()
	metadata, err := identifyMergeWorkspace(ctx, workspaceRoot, managedRoot, &inspection)
	if err != nil {
		return unavailableInspection(inspection, err.Error())
	}
	worktreeStatus, err := inspectCheckoutStates(ctx, metadata, &inspection)
	if err != nil {
		return unavailableInspection(inspection, err.Error())
	}
	if err := inspectMergeDivergence(ctx, metadata, worktreeStatus, &inspection); err != nil {
		return unavailableInspection(inspection, err.Error())
	}
	if err := inspectCleanupBlockers(ctx, metadata, &inspection); err != nil {
		return unavailableInspection(inspection, err.Error())
	}
	inspection.CanMerge = !hasBlockingMergeIssue(inspection.Blockers)
	return inspection, nil
}

func identifyMergeWorkspace(ctx context.Context, workspaceRoot, managedRoot string, inspection *MergeInspection) (mergeMetadata, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return mergeMetadata{}, errors.New("workspace root is required")
	}
	worktreeRoot, stderr, err := runGit(ctx, workspaceRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return mergeMetadata{}, fmt.Errorf("resolve worktree root: %w%s", err, stderrSuffix(stderr))
	}
	worktreeRoot = filepath.Clean(strings.TrimSpace(worktreeRoot))
	metadata, _, err := readMergeMetadata(worktreeRoot, managedRoot)
	if err != nil {
		return mergeMetadata{}, err
	}
	*inspection = emptyMergeInspection()
	inspection.Available = true
	inspection.WorktreeRoot, inspection.SourceRoot = metadata.WorktreeRoot, metadata.SourceRoot
	inspection.WorktreeBranch, inspection.TargetBranch = metadata.WorktreeBranch, metadata.TargetBranch
	inspection.CreatedHead = metadata.CreatedHead
	if err := sameDirectory(worktreeRoot, metadata.WorktreeRoot); err != nil {
		return mergeMetadata{}, errors.New("workspace is not the metadata worktree root")
	}
	if err := verifySameCommonDir(ctx, metadata.SourceRoot, metadata.WorktreeRoot); err != nil {
		return mergeMetadata{}, err
	}
	if err := verifyRepositoryRoot(ctx, metadata.SourceRoot); err != nil {
		return mergeMetadata{}, fmt.Errorf("source checkout identity changed: %w", err)
	}
	if metadata.TargetBranch == "" {
		inspection.Blockers = append(inspection.Blockers, blocker("target_branch_missing", "creation metadata does not contain a target branch"))
	} else if _, _, err := runGit(ctx, metadata.SourceRoot, "check-ref-format", "refs/heads/"+metadata.TargetBranch); err != nil {
		inspection.Blockers = append(inspection.Blockers, blocker("target_branch_missing", "creation metadata does not contain a valid target branch"))
	}
	if _, _, err := runGit(ctx, metadata.WorktreeRoot, "check-ref-format", "refs/heads/"+metadata.WorktreeBranch); err != nil {
		return mergeMetadata{}, errors.New("worktree metadata contains an invalid branch")
	}
	return metadata, nil
}

func inspectCheckoutStates(ctx context.Context, metadata mergeMetadata, inspection *MergeInspection) (string, error) {
	worktreeBranch, stderr, err := gitValue(ctx, metadata.WorktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("worktree is detached or unreadable%s", stderrSuffix(stderr))
	}
	if worktreeBranch != metadata.WorktreeBranch {
		return "", fmt.Errorf("worktree branch changed from %q to %q", metadata.WorktreeBranch, worktreeBranch)
	}
	inspection.WorktreeHead, stderr, err = gitValue(ctx, metadata.WorktreeRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read worktree HEAD: %w%s", err, stderrSuffix(stderr))
	}
	sourceBranch, _, branchErr := gitValue(ctx, metadata.SourceRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branchErr != nil {
		inspection.Blockers = append(inspection.Blockers, blocker("source_detached", "the recorded source checkout is detached"))
	} else if sourceBranch != metadata.TargetBranch {
		inspection.Blockers = append(inspection.Blockers, blocker("target_branch_drift", fmt.Sprintf("source checkout is on %q, expected %q", sourceBranch, metadata.TargetBranch)))
	}
	inspection.TargetHead, stderr, err = gitValue(ctx, metadata.SourceRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read target HEAD: %w%s", err, stderrSuffix(stderr))
	}
	worktreeStatus, stderr, err := runGit(ctx, metadata.WorktreeRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("inspect worktree changes: %w%s", err, stderrSuffix(stderr))
	}
	inspection.WorktreeDirty = strings.TrimSpace(worktreeStatus) != ""
	inspection.WorktreeStateToken, err = worktreeStateToken(ctx, metadata.WorktreeRoot)
	if err != nil {
		return "", fmt.Errorf("snapshot worktree changes: %w", err)
	}
	if inspection.WorktreeDirty {
		inspection.Blockers = append(inspection.Blockers, blocker("worktree_dirty", "worktree has uncommitted changes"))
	}
	if err := inspectSourceState(ctx, metadata.SourceRoot, inspection); err != nil {
		return "", err
	}
	return worktreeStatus, nil
}

func inspectSourceState(ctx context.Context, sourceRoot string, inspection *MergeInspection) error {
	status, stderr, err := runGit(ctx, sourceRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect source changes: %w%s", err, stderrSuffix(stderr))
	}
	inspection.SourceDirty = strings.TrimSpace(status) != ""
	if inspection.SourceDirty {
		inspection.Blockers = append(inspection.Blockers, blocker("source_dirty", "the recorded source checkout has uncommitted changes"))
	}
	operation, err := gitOperation(ctx, sourceRoot)
	if err != nil {
		return err
	}
	if operation != "" {
		inspection.Blockers = append(inspection.Blockers, blocker("source_operation", "the source checkout has an in-progress Git "+operation))
	}
	return nil
}

func inspectMergeDivergence(ctx context.Context, metadata mergeMetadata, worktreeStatus string, inspection *MergeInspection) error {
	if metadata.TargetBranch == "" {
		return nil
	}
	behind, ahead, err := aheadBehind(ctx, metadata.WorktreeRoot, inspection.TargetHead, inspection.WorktreeHead)
	if err != nil {
		return err
	}
	inspection.AheadCount, inspection.BehindCount = ahead, behind
	inspection.FilesChanged, inspection.Insertions, inspection.Deletions, inspection.ChangedFiles, err = diffStats(ctx, metadata.WorktreeRoot, inspection.TargetHead, inspection.WorktreeHead, worktreeStatus)
	if err != nil {
		return err
	}
	inspection.AlreadyMerged, err = isAncestor(ctx, metadata.SourceRoot, inspection.WorktreeHead, inspection.TargetHead)
	if err != nil {
		return fmt.Errorf("check merged ancestry: %w", err)
	}
	if inspection.AlreadyMerged {
		return nil
	}
	_, inspection.HasConflicts, inspection.ConflictFiles, err = mergeTree(ctx, metadata.SourceRoot, inspection.TargetHead, inspection.WorktreeHead)
	if err != nil {
		return err
	}
	if inspection.HasConflicts {
		inspection.Blockers = append(inspection.Blockers, MergeBlocker{Code: "merge_conflict", Message: "the branches do not merge cleanly", Paths: inspection.ConflictFiles})
	}
	return nil
}

func inspectCleanupBlockers(ctx context.Context, metadata mergeMetadata, inspection *MergeInspection) error {
	status, stderr, err := runGit(ctx, metadata.WorktreeRoot, "status", "--porcelain=v1", "--untracked-files=all", "--ignored")
	if err != nil {
		return fmt.Errorf("inspect cleanup safety: %w%s", err, stderrSuffix(stderr))
	}
	if paths := statusPaths(status); len(paths) > 0 {
		inspection.CleanupBlockers = append(inspection.CleanupBlockers, MergeBlocker{Code: "worktree_content", Message: "tracked, untracked, or ignored files would be preserved", Paths: paths})
	}
	if !inspection.AlreadyMerged {
		inspection.CleanupBlockers = append(inspection.CleanupBlockers, blocker("not_merged", "worktree HEAD is not contained in the target branch"))
	}
	return nil
}

// MergeBack commits opted-in dirty changes, re-runs inspection, and merges.
// It never removes the worktree or its branch.
func MergeBack(ctx context.Context, managedRoot string, request MergeRequest) (MergeResult, error) {
	inspection, err := InspectMerge(ctx, request.WorkspaceRoot, managedRoot)
	if err != nil {
		return mergeFailure(inspection, false, err)
	}
	if err := verifyExpectedInspection(inspection, request); err != nil {
		return mergeFailure(inspection, false, err)
	}
	if inspection.WorktreeDirty {
		if !request.AutoCommitDirty {
			return mergeFailure(inspection, false, errors.New("worktree has uncommitted changes; explicit auto-commit is required"))
		}
		confirmedToken, tokenErr := worktreeStateToken(ctx, inspection.WorktreeRoot)
		if tokenErr != nil {
			return mergeFailure(inspection, false, fmt.Errorf("verify confirmed worktree changes: %w", tokenErr))
		}
		if confirmedToken != request.ExpectedWorktreeStateToken {
			return mergeFailure(inspection, false, errors.New("worktree contents changed after confirmation; inspect and confirm again"))
		}
		originalWorktreeHead := inspection.WorktreeHead
		if _, stderr, err := runGit(ctx, inspection.WorktreeRoot, "add", "-A"); err != nil {
			return mergeFailure(inspection, false, fmt.Errorf("stage worktree changes: %w%s", err, stderrSuffix(stderr)))
		}
		noteMergeStep("after_worktree_add")
		stagedTree, err := verifyStagedWorktree(ctx, inspection.WorktreeRoot, confirmedToken)
		if err != nil {
			return mergeFailure(inspection, false, fmt.Errorf("worktree changed while staging; staged changes were preserved: %w", err))
		}
		if _, stderr, err := runGit(ctx, inspection.WorktreeRoot, "-c", "user.name=Reasonix", "-c", "user.email=reasonix@local", "commit", "-m", "worktree: save changes before merge back"); err != nil {
			return mergeFailure(inspection, false, fmt.Errorf("commit worktree changes: %w%s", err, stderrSuffix(stderr)))
		}
		noteMergeStep("after_worktree_commit")
		committedHead, err := verifyAutoCommit(ctx, inspection.WorktreeRoot, originalWorktreeHead, stagedTree)
		if err != nil {
			return mergeFailure(inspection, false, fmt.Errorf("auto-commit identity changed; worktree and commits were preserved: %w", err))
		}
		inspection, err = InspectMerge(ctx, request.WorkspaceRoot, managedRoot)
		if err != nil {
			return mergeFailure(inspection, false, fmt.Errorf("re-inspect after auto-commit: %w", err))
		}
		if inspection.WorktreeHead != committedHead {
			return mergeFailure(inspection, false, errors.New("worktree HEAD changed after Reasonix auto-commit; inspect again"))
		}
		request.ExpectedWorktreeHead = committedHead
		request.ExpectedWorktreeStateToken = inspection.WorktreeStateToken
		if err := verifyExpectedInspection(inspection, request); err != nil {
			return mergeFailure(inspection, false, err)
		}
	}
	if !inspection.CanMerge {
		return mergeFailure(inspection, false, fmt.Errorf("merge is blocked: %s", blockerMessages(inspection.Blockers)))
	}
	if inspection.AlreadyMerged {
		return mergeReceipt(inspection, inspection.TargetHead, true), nil
	}

	mergedHead, recoveryRequired, err := mergeSourceCheckout(ctx, inspection)
	if err != nil {
		return mergeFailure(inspection, recoveryRequired, err)
	}
	return mergeReceipt(inspection, mergedHead, false), nil
}

// FinalizeMerge removes only an exact, clean, fully merged worktree, then uses
// normal branch deletion. Callers must separately prove there are no runtime
// or visible-tab references before invoking it.
func FinalizeMerge(ctx context.Context, managedRoot string, request CleanupRequest) (CleanupResult, error) {
	result := CleanupResult{Blockers: []MergeBlocker{}}
	metadata, metadataFile, rootExists, err := readMergeMetadataForCleanup(request.WorktreeRoot, managedRoot)
	if err != nil {
		return cleanupFailure(result, err)
	}
	if err := verifyCleanupIdentity(metadata, request); err != nil {
		return cleanupFailure(result, err)
	}
	if err := verifyRepositoryRoot(ctx, metadata.SourceRoot); err != nil {
		return cleanupFailure(result, fmt.Errorf("source checkout identity changed: %w", err))
	}
	sourceBranch, stderr, err := gitValue(ctx, metadata.SourceRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || sourceBranch != metadata.TargetBranch {
		return cleanupFailure(result, fmt.Errorf("source checkout is not on target branch %q%s", metadata.TargetBranch, stderrSuffix(stderr)))
	}
	targetHead, stderr, err := gitValue(ctx, metadata.SourceRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return cleanupFailure(result, fmt.Errorf("read target HEAD: %w%s", err, stderrSuffix(stderr)))
	}
	if ok, err := isAncestor(ctx, metadata.SourceRoot, request.MergedCommit, targetHead); err != nil || !ok {
		return cleanupFailure(result, errors.New("the recorded merge commit is no longer contained in the target branch"))
	}
	if ok, err := isAncestor(ctx, metadata.SourceRoot, request.WorktreeHead, targetHead); err != nil || !ok {
		return cleanupFailure(result, errors.New("worktree HEAD is not contained in the target branch"))
	}

	if rootExists {
		branch, stderr, err := gitValue(ctx, metadata.WorktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
		if err != nil || branch != metadata.WorktreeBranch {
			return cleanupFailure(result, fmt.Errorf("worktree branch identity changed%s", stderrSuffix(stderr)))
		}
		head, stderr, err := gitValue(ctx, metadata.WorktreeRoot, "rev-parse", "--verify", "HEAD")
		if err != nil || head != request.WorktreeHead {
			return cleanupFailure(result, fmt.Errorf("worktree HEAD identity changed%s", stderrSuffix(stderr)))
		}
		status, stderr, err := runGit(ctx, metadata.WorktreeRoot, "status", "--porcelain=v1", "--untracked-files=all", "--ignored")
		if err != nil {
			return cleanupFailure(result, fmt.Errorf("inspect cleanup safety: %w%s", err, stderrSuffix(stderr)))
		}
		if paths := statusPaths(status); len(paths) > 0 {
			result.Blockers = append(result.Blockers, MergeBlocker{Code: "worktree_content", Message: "tracked, untracked, or ignored files block cleanup", Paths: paths})
			result.Error = "worktree contains files that must be preserved"
			return result, errors.New(result.Error)
		}
		if _, stderr, err := runGit(ctx, metadata.SourceRoot, "worktree", "remove", metadata.WorktreeRoot); err != nil {
			return cleanupFailure(result, fmt.Errorf("remove clean worktree: %w%s", err, stderrSuffix(stderr)))
		}
		result.WorktreeRemoved = true
	} else {
		result.WorktreeRemoved = true
	}

	_, stderr, err = runGit(ctx, metadata.SourceRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+metadata.WorktreeBranch)
	if err == nil {
		branchHead, branchStderr, branchErr := gitValue(ctx, metadata.SourceRoot, "rev-parse", "--verify", "refs/heads/"+metadata.WorktreeBranch)
		if branchErr != nil {
			return cleanupFailure(result, fmt.Errorf("inspect temporary branch HEAD: %w%s", branchErr, stderrSuffix(branchStderr)))
		}
		if branchHead != request.WorktreeHead {
			return cleanupFailure(result, errors.New("temporary branch moved after merge; it was preserved"))
		}
		if _, stderr, err := runGit(ctx, metadata.SourceRoot, "branch", "-d", metadata.WorktreeBranch); err != nil {
			return cleanupFailure(result, fmt.Errorf("delete merged temporary branch: %w%s", err, stderrSuffix(stderr)))
		}
		result.BranchDeleted = true
	} else if exitCode(err) == 1 {
		result.BranchDeleted = true
	} else {
		return cleanupFailure(result, fmt.Errorf("inspect temporary branch: %w%s", err, stderrSuffix(stderr)))
	}
	if err := os.Remove(metadataFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return cleanupFailure(result, fmt.Errorf("remove completed merge metadata: %w", err))
	}
	result.Completed = true
	return result, nil
}

func emptyMergeInspection() MergeInspection {
	return MergeInspection{ChangedFiles: []string{}, ConflictFiles: []string{}, Blockers: []MergeBlocker{}, CleanupBlockers: []MergeBlocker{}}
}

func unavailableInspection(inspection MergeInspection, reason string) (MergeInspection, error) {
	inspection.Available = false
	inspection.CanMerge = false
	inspection.Reason = reason
	inspection.Blockers = append(inspection.Blockers, blocker("identity", reason))
	return inspection, errors.New(reason)
}

func blocker(code, message string) MergeBlocker {
	return MergeBlocker{Code: code, Message: message, Paths: []string{}}
}

func verifyRepositoryRoot(ctx context.Context, expected string) error {
	reported, stderr, err := runGit(ctx, expected, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolve repository root: %w%s", err, stderrSuffix(stderr))
	}
	return sameDirectory(expected, strings.TrimSpace(reported))
}

func gitValue(ctx context.Context, dir string, args ...string) (string, string, error) {
	out, stderr, err := runGit(ctx, dir, args...)
	return strings.TrimSpace(out), stderr, err
}

func aheadBehind(ctx context.Context, root, targetHead, worktreeHead string) (behind, ahead int, err error) {
	out, stderr, err := runGit(ctx, root, "rev-list", "--left-right", "--count", targetHead+"..."+worktreeHead)
	if err != nil {
		return 0, 0, fmt.Errorf("inspect branch divergence: %w%s", err, stderrSuffix(stderr))
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("inspect branch divergence: unexpected output %q", strings.TrimSpace(out))
	}
	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse behind count: %w", err)
	}
	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse ahead count: %w", err)
	}
	return behind, ahead, nil
}

func diffStats(ctx context.Context, root, targetHead, worktreeHead, status string) (files, insertions, deletions int, paths []string, err error) {
	paths = []string{}
	seen := map[string]struct{}{}
	out, stderr, err := runGit(ctx, root, "diff", "--numstat", targetHead+"..."+worktreeHead)
	if err != nil {
		return 0, 0, 0, paths, fmt.Errorf("inspect committed diff: %w%s", err, stderrSuffix(stderr))
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return 0, 0, 0, paths, fmt.Errorf("inspect committed diff: unexpected numstat %q", line)
		}
		if value, parseErr := strconv.Atoi(fields[0]); parseErr == nil {
			insertions += value
		}
		if value, parseErr := strconv.Atoi(fields[1]); parseErr == nil {
			deletions += value
		}
		if _, ok := seen[fields[2]]; !ok {
			seen[fields[2]] = struct{}{}
			paths = append(paths, fields[2])
		}
	}
	for _, path := range statusPaths(status) {
		if _, ok := seen[path]; !ok {
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return len(paths), insertions, deletions, paths, nil
}

func statusPaths(status string) []string {
	seen := map[string]struct{}{}
	paths := []string{}
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = strings.TrimSpace(path[arrow+4:])
		}
		path = strings.Trim(path, "\"")
		if path != "" {
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				paths = append(paths, path)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func isAncestor(ctx context.Context, root, ancestor, descendant string) (bool, error) {
	_, stderr, err := runGit(ctx, root, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w%s", err, stderrSuffix(stderr))
}

func hasBlockingMergeIssue(blockers []MergeBlocker) bool {
	return len(blockers) > 0
}

func verifyExpectedInspection(inspection MergeInspection, request MergeRequest) error {
	if request.ExpectedTargetBranch == "" || request.ExpectedTargetHead == "" || request.ExpectedWorktreeHead == "" || request.ExpectedWorktreeStateToken == "" {
		return errors.New("merge confirmation identity is incomplete; inspect again")
	}
	if inspection.TargetBranch != request.ExpectedTargetBranch || inspection.TargetHead != request.ExpectedTargetHead || inspection.WorktreeHead != request.ExpectedWorktreeHead || inspection.WorktreeStateToken != request.ExpectedWorktreeStateToken {
		return errors.New("merge identity changed after inspection; inspect and confirm again")
	}
	return nil
}

func blockerMessages(blockers []MergeBlocker) string {
	items := make([]string, 0, len(blockers))
	for _, item := range blockers {
		items = append(items, item.Message)
	}
	return strings.Join(items, "; ")
}

func mergeReceipt(inspection MergeInspection, mergedHead string, alreadyMerged bool) MergeResult {
	return MergeResult{
		Merged: true, AlreadyMerged: alreadyMerged, SourceRoot: inspection.SourceRoot,
		TargetBranch: inspection.TargetBranch, TargetHead: mergedHead, MergedCommit: mergedHead,
		WorktreeRoot: inspection.WorktreeRoot, WorktreeBranch: inspection.WorktreeBranch, WorktreeHead: inspection.WorktreeHead,
	}
}

func mergeFailure(inspection MergeInspection, recoveryRequired bool, err error) (MergeResult, error) {
	result := mergeReceipt(inspection, "", inspection.AlreadyMerged)
	result.Merged = false
	result.RecoveryRequired = recoveryRequired
	result.Error = err.Error()
	return result, err
}

func verifyCleanupIdentity(metadata mergeMetadata, request CleanupRequest) error {
	if strings.TrimSpace(request.SourceRoot) == "" || strings.TrimSpace(request.TargetBranch) == "" || strings.TrimSpace(request.MergedCommit) == "" ||
		strings.TrimSpace(request.WorktreeBranch) == "" || strings.TrimSpace(request.WorktreeHead) == "" {
		return errors.New("cleanup identity is incomplete")
	}
	if err := sameDirectory(metadata.SourceRoot, request.SourceRoot); err != nil {
		return errors.New("cleanup source identity changed")
	}
	if metadata.TargetBranch != request.TargetBranch || metadata.WorktreeBranch != request.WorktreeBranch {
		return errors.New("cleanup branch identity changed")
	}
	return nil
}

func cleanupFailure(result CleanupResult, err error) (CleanupResult, error) {
	result.Error = err.Error()
	return result, err
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
