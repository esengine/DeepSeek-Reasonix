package worktree

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func mergeSourceCheckout(ctx context.Context, inspection MergeInspection) (string, bool, error) {
	originalHead := inspection.TargetHead
	message := fmt.Sprintf("Merge worktree branch '%s' into %s", inspection.WorktreeBranch, inspection.TargetBranch)
	noteMergeStep("before_merge_prepare")
	if err := verifySourceIdentity(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead, false); err != nil {
		return "", false, fmt.Errorf("source changed before merge preparation: %w", err)
	}
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", false, fmt.Errorf("worktree changed before merge preparation: %w", err)
	}
	expectedTree, hasConflicts, conflictFiles, err := mergeTree(ctx, inspection.SourceRoot, originalHead, inspection.WorktreeHead)
	if err != nil {
		return "", false, fmt.Errorf("recompute source merge tree: %w", err)
	}
	if hasConflicts {
		return "", false, fmt.Errorf("source merge conflicts changed after inspection: %s", strings.Join(conflictFiles, ", "))
	}
	if _, stderr, err := runGit(ctx, inspection.SourceRoot, "merge", "--no-ff", "--no-commit", inspection.WorktreeHead); err != nil {
		recovered, recoveryErr := abortAndVerifyMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead)
		if !recovered {
			return "", true, fmt.Errorf("merge failed: %w%s; automatic recovery failed: %v", err, stderrSuffix(stderr), recoveryErr)
		}
		return "", false, fmt.Errorf("merge failed and was aborted: %w%s", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_merge_prepare")
	if err := verifyPreparedMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead, inspection.WorktreeHead, expectedTree); err != nil {
		return "", true, fmt.Errorf("merge preparation identity changed; source state was preserved for recovery: %w", err)
	}
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return abortPreparedWorktreeDrift(ctx, inspection, originalHead, err)
	}
	mergedHead, stderr, err := gitValue(ctx, inspection.SourceRoot,
		"-c", "user.name=Reasonix", "-c", "user.email=reasonix@local",
		"commit-tree", expectedTree, "-p", originalHead, "-p", inspection.WorktreeHead, "-m", message)
	if err != nil {
		recovered, recoveryErr := abortAndVerifyMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead)
		if !recovered {
			return "", true, fmt.Errorf("create exact merge commit: %w%s; automatic recovery failed: %v", err, stderrSuffix(stderr), recoveryErr)
		}
		return "", false, fmt.Errorf("create exact merge commit: %w%s; merge was aborted", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_merge_commit_object")
	if err := verifyPreparedMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead, inspection.WorktreeHead, expectedTree); err != nil {
		return "", true, fmt.Errorf("source changed before target ref update; source state was preserved for recovery: %w", err)
	}
	noteMergeStep("before_merge_ref_update")
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return abortPreparedWorktreeDrift(ctx, inspection, originalHead, err)
	}
	noteMergeStep("before_merge_ref_transaction")
	if stderr, err := updateMergeRefs(ctx, inspection, originalHead, mergedHead); err != nil {
		return recoverRefUpdateFailure(ctx, inspection, originalHead, expectedTree, stderr, err)
	}
	noteMergeStep("after_merge_ref_update")
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", true, fmt.Errorf("merge commit was installed but the worktree identity changed; recovery is required: %w", err)
	}
	if _, stderr, err := runGit(ctx, inspection.SourceRoot, "update-ref", "-d", "MERGE_HEAD", inspection.WorktreeHead); err != nil {
		return "", true, fmt.Errorf("merge commit was installed but MERGE_HEAD changed; source requires recovery: %w%s", err, stderrSuffix(stderr))
	}
	if _, stderr, err := runGit(ctx, inspection.SourceRoot, "reset", "--quiet", "--soft", "HEAD"); err != nil {
		return "", true, fmt.Errorf("merge commit was installed but merge state cleanup failed; source requires recovery: %w%s", err, stderrSuffix(stderr))
	}
	noteMergeStep("after_merge_commit")
	verifiedHead, err := verifySuccessfulMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead, inspection.WorktreeHead, expectedTree)
	if err != nil {
		return "", true, fmt.Errorf("merge succeeded but the source checkout requires recovery: %w", err)
	}
	if err := verifyWorktreeMergeIdentity(ctx, inspection); err != nil {
		return "", true, fmt.Errorf("merge succeeded but the worktree identity changed; recovery is required: %w", err)
	}
	return verifiedHead, false, nil
}

func abortPreparedWorktreeDrift(ctx context.Context, inspection MergeInspection, originalHead string, driftErr error) (string, bool, error) {
	recovered, recoveryErr := abortAndVerifyMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead)
	if recovered {
		return "", false, fmt.Errorf("worktree changed before target ref update; merge was aborted: %w", driftErr)
	}
	return "", true, fmt.Errorf("worktree changed before target ref update: %w; automatic recovery failed: %v", driftErr, recoveryErr)
}

func updateMergeRefs(ctx context.Context, inspection MergeInspection, originalHead, mergedHead string) (string, error) {
	targetRef := "refs/heads/" + inspection.TargetBranch
	worktreeRef := "refs/heads/" + inspection.WorktreeBranch
	input := fmt.Sprintf("verify %s %s\nupdate %s %s %s\n", worktreeRef, inspection.WorktreeHead, targetRef, mergedHead, originalHead)
	_, stderr, err := runGitInput(ctx, inspection.SourceRoot, input, "update-ref", "--stdin")
	return stderr, err
}

func recoverRefUpdateFailure(ctx context.Context, inspection MergeInspection, originalHead, expectedTree, stderr string, updateErr error) (string, bool, error) {
	targetRef, targetStderr, targetErr := gitValue(ctx, inspection.SourceRoot, "rev-parse", "--verify", "refs/heads/"+inspection.TargetBranch)
	if targetErr != nil || targetRef != originalHead {
		return "", true, fmt.Errorf("target ref changed during compare-and-swap; source requires recovery: %w%s%s", updateErr, stderrSuffix(stderr), stderrSuffix(targetStderr))
	}
	if verifyErr := verifyPreparedMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead, inspection.WorktreeHead, expectedTree); verifyErr != nil {
		return "", true, fmt.Errorf("target ref changed during compare-and-swap; source requires recovery: %w%s", updateErr, stderrSuffix(stderr))
	}
	recovered, recoveryErr := abortAndVerifyMerge(ctx, inspection.SourceRoot, inspection.TargetBranch, originalHead)
	if recovered {
		return "", false, fmt.Errorf("target ref update failed and merge was aborted: %w%s", updateErr, stderrSuffix(stderr))
	}
	return "", true, fmt.Errorf("target ref update failed: %w%s; automatic recovery failed: %v", updateErr, stderrSuffix(stderr), recoveryErr)
}

func verifyWorktreeMergeIdentity(ctx context.Context, inspection MergeInspection) error {
	if err := verifyRepositoryRoot(ctx, inspection.WorktreeRoot); err != nil {
		return fmt.Errorf("worktree checkout identity changed: %w", err)
	}
	if err := verifySameCommonDir(ctx, inspection.SourceRoot, inspection.WorktreeRoot); err != nil {
		return fmt.Errorf("worktree repository identity changed: %w", err)
	}
	branch, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != inspection.WorktreeBranch {
		return fmt.Errorf("worktree branch is %q, expected %q%s", branch, inspection.WorktreeBranch, stderrSuffix(stderr))
	}
	branchHead, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "rev-parse", "--verify", "refs/heads/"+inspection.WorktreeBranch)
	if err != nil || branchHead != inspection.WorktreeHead {
		return fmt.Errorf("worktree branch HEAD changed from %s to %s%s", inspection.WorktreeHead, branchHead, stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, inspection.WorktreeRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != inspection.WorktreeHead {
		return fmt.Errorf("worktree HEAD changed from %s to %s%s", inspection.WorktreeHead, head, stderrSuffix(stderr))
	}
	operation, err := gitOperation(ctx, inspection.WorktreeRoot)
	if err != nil {
		return err
	}
	if operation != "" {
		return fmt.Errorf("worktree Git %s operation is in progress", operation)
	}
	token, err := worktreeStateToken(ctx, inspection.WorktreeRoot)
	if err != nil {
		return fmt.Errorf("snapshot worktree contents: %w", err)
	}
	if token != inspection.WorktreeStateToken {
		return errors.New("worktree contents changed after confirmation")
	}
	return nil
}

func mergeTree(ctx context.Context, root, targetHead, worktreeHead string) (string, bool, []string, error) {
	out, stderr, err := runGit(ctx, root, "merge-tree", "--write-tree", "--name-only", targetHead, worktreeHead)
	lines := strings.Split(out, "\n")
	tree := ""
	if len(lines) > 0 {
		tree = strings.TrimSpace(lines[0])
	}
	if err == nil {
		if !isHexObject(tree) {
			return "", false, []string{}, errors.New("preflight merge did not produce a tree")
		}
		return tree, false, []string{}, nil
	}
	if exitCode(err) != 1 {
		return "", false, []string{}, fmt.Errorf("preflight merge conflicts: %w%s", err, stderrSuffix(stderr))
	}
	paths := []string{}
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if index == 0 || line == "" || strings.Contains(line, " ") || isHexObject(line) {
			continue
		}
		paths = append(paths, line)
	}
	sort.Strings(paths)
	return tree, true, paths, nil
}

func isHexObject(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}

func verifySourceIdentity(ctx context.Context, sourceRoot, targetBranch, originalHead string, expectMerge bool) error {
	branch, stderr, err := gitValue(ctx, sourceRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != targetBranch {
		return fmt.Errorf("source branch is %q, expected %q%s", branch, targetBranch, stderrSuffix(stderr))
	}
	head, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != originalHead {
		return fmt.Errorf("source HEAD changed from %s to %s%s", originalHead, head, stderrSuffix(stderr))
	}
	operation, err := gitOperation(ctx, sourceRoot)
	if err != nil {
		return err
	}
	if expectMerge && operation != "merge" {
		return fmt.Errorf("prepared merge operation is missing (found %q)", operation)
	}
	if !expectMerge && operation != "" {
		return fmt.Errorf("source Git %s operation is already in progress", operation)
	}
	if !expectMerge {
		status, stderr, err := runGit(ctx, sourceRoot, "status", "--porcelain=v1", "--untracked-files=all")
		if err != nil {
			return fmt.Errorf("inspect source status: %w%s", err, stderrSuffix(stderr))
		}
		if strings.TrimSpace(status) != "" {
			return errors.New("source checkout is no longer clean")
		}
	}
	return nil
}

func verifyPreparedMerge(ctx context.Context, sourceRoot, targetBranch, originalHead, worktreeHead, expectedTree string) error {
	if err := verifySourceIdentity(ctx, sourceRoot, targetBranch, originalHead, true); err != nil {
		return err
	}
	mergeHead, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "MERGE_HEAD")
	if err != nil {
		return fmt.Errorf("read prepared MERGE_HEAD: %w%s", err, stderrSuffix(stderr))
	}
	if mergeHead != worktreeHead {
		return fmt.Errorf("prepared MERGE_HEAD is %s, expected %s", mergeHead, worktreeHead)
	}
	preparedTree, stderr, err := gitValue(ctx, sourceRoot, "write-tree")
	if err != nil {
		return fmt.Errorf("read prepared merge tree: %w%s", err, stderrSuffix(stderr))
	}
	if preparedTree != expectedTree {
		return fmt.Errorf("prepared merge tree is %s, expected %s", preparedTree, expectedTree)
	}
	return nil
}

func abortAndVerifyMerge(ctx context.Context, sourceRoot, targetBranch, originalHead string) (bool, error) {
	operation, operationErr := gitOperation(ctx, sourceRoot)
	if operationErr != nil {
		return false, operationErr
	}
	if operation == "merge" {
		if _, stderr, err := runGit(ctx, sourceRoot, "merge", "--abort"); err != nil {
			return false, fmt.Errorf("git merge --abort: %w%s", err, stderrSuffix(stderr))
		}
	}
	if err := verifySourceIdentity(ctx, sourceRoot, targetBranch, originalHead, false); err != nil {
		return false, err
	}
	branchRef, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "refs/heads/"+targetBranch)
	if err != nil || branchRef != originalHead {
		return false, fmt.Errorf("target branch ref was not restored%s", stderrSuffix(stderr))
	}
	status, stderr, err := runGit(ctx, sourceRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || strings.TrimSpace(status) != "" {
		return false, fmt.Errorf("source checkout was not restored clean%s", stderrSuffix(stderr))
	}
	operation, err = gitOperation(ctx, sourceRoot)
	if err != nil || operation != "" {
		return false, fmt.Errorf("source Git operation remains after abort: %s", operation)
	}
	return true, nil
}

func verifySuccessfulMerge(ctx context.Context, sourceRoot, targetBranch, originalHead, worktreeHead, expectedTree string) (string, error) {
	branch, stderr, err := gitValue(ctx, sourceRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != targetBranch {
		return "", fmt.Errorf("source branch changed after merge; found %q, expected %q%s", branch, targetBranch, stderrSuffix(stderr))
	}
	mergedHead, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read merged target HEAD: %w%s", err, stderrSuffix(stderr))
	}
	branchHead, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", "refs/heads/"+targetBranch)
	if err != nil || branchHead != mergedHead {
		return "", fmt.Errorf("target branch ref does not identify the merge commit%s", stderrSuffix(stderr))
	}
	commitTree, stderr, err := gitValue(ctx, sourceRoot, "rev-parse", "--verify", mergedHead+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("read merge commit tree: %w%s", err, stderrSuffix(stderr))
	}
	if commitTree != expectedTree {
		return "", errors.New("merge commit tree differs from the exact prepared tree")
	}
	indexTree, stderr, err := gitValue(ctx, sourceRoot, "write-tree")
	if err != nil {
		return "", fmt.Errorf("read merged source index tree: %w%s", err, stderrSuffix(stderr))
	}
	if indexTree != expectedTree {
		return "", errors.New("merged source index differs from the exact prepared tree")
	}
	parents, stderr, err := gitValue(ctx, sourceRoot, "rev-list", "--parents", "-n", "1", mergedHead)
	if err != nil {
		return "", fmt.Errorf("read merge commit parents: %w%s", err, stderrSuffix(stderr))
	}
	fields := strings.Fields(parents)
	if len(fields) != 3 || fields[0] != mergedHead || fields[1] != originalHead || fields[2] != worktreeHead {
		return "", errors.New("merge commit does not have the exact prepared parents")
	}
	for _, ancestor := range []struct{ label, head string }{{"original target", originalHead}, {"worktree", worktreeHead}} {
		contained, ancestorErr := isAncestor(ctx, sourceRoot, ancestor.head, mergedHead)
		if ancestorErr != nil {
			return "", fmt.Errorf("verify %s ancestry: %w", ancestor.label, ancestorErr)
		}
		if !contained {
			return "", fmt.Errorf("%s HEAD is not contained in merged target", ancestor.label)
		}
	}
	status, stderr, err := runGit(ctx, sourceRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", fmt.Errorf("verify merged source status: %w%s", err, stderrSuffix(stderr))
	}
	if strings.TrimSpace(status) != "" {
		return "", errors.New("merged source checkout is not clean")
	}
	operation, err := gitOperation(ctx, sourceRoot)
	if err != nil {
		return "", err
	}
	if operation != "" {
		return "", fmt.Errorf("source Git %s operation remains after merge", operation)
	}
	return mergedHead, nil
}
