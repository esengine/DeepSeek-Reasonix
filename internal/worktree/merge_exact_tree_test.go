package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMergeBackUsesExactPreparedTreeWithoutHooks(t *testing.T) {
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("executable Git hook permissions are not portable to Windows")
	}
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	expectedTree, conflicts, _, err := mergeTree(context.Background(), repo, inspection.TargetHead, inspection.WorktreeHead)
	if err != nil || conflicts {
		t.Fatalf("expected merge tree = %q, conflicts=%v, err=%v", expectedTree, conflicts, err)
	}

	hookPath := gitTest(t, repo, "rev-parse", "--git-path", "hooks/pre-commit")
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(repo, hookPath)
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err != nil || !result.Merged {
		t.Fatalf("exact-tree merge = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD^{tree}"); got != expectedTree {
		t.Fatalf("merge commit tree = %s, want %s", got, expectedTree)
	}
	parents := strings.Fields(gitTest(t, repo, "rev-list", "--parents", "-n", "1", result.MergedCommit))
	if len(parents) != 3 || parents[1] != inspection.TargetHead || parents[2] != inspection.WorktreeHead {
		t.Fatalf("merge parents = %v", parents)
	}
}

func TestMergeBackDoesNotRequireUserGitIdentity(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	gitTest(t, repo, "config", "user.useConfigOnly", "true")
	gitTest(t, repo, "config", "user.name", "")
	gitTest(t, repo, "config", "user.email", "")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err != nil || !result.Merged || result.RecoveryRequired {
		t.Fatalf("identity-free merge = %+v, %v", result, err)
	}
}

func TestMergeBackFailsClosedWhenSourceMutationFenceIsBusy(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	indexPath := gitTest(t, repo, "rev-parse", "--git-path", "index")
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(repo, indexPath)
	}
	lockPath := indexPath + ".lock"
	mergeStepHook = func(step string) {
		if step == "before_merge_ref_update" {
			if err := os.WriteFile(lockPath, []byte("held\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() {
		mergeStepHook = nil
		_ = os.Remove(lockPath)
	})

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "mutation fence") {
		t.Fatalf("busy source fence = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+inspection.TargetBranch); got != inspection.TargetHead {
		t.Fatalf("target advanced while source fence was busy: %s", got)
	}
}

func TestMergeBackRejectsSameHeadSourceBranchSwitchBeforeCAS(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	mergeStepHook = func(step string) {
		if step == "before_merge_ref_update" {
			gitTest(t, repo, "update-ref", "refs/heads/same-head-external", inspection.TargetHead)
			gitTest(t, repo, "symbolic-ref", "HEAD", "refs/heads/same-head-external")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "source changed") {
		t.Fatalf("same-head source switch = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+inspection.TargetBranch); got != inspection.TargetHead {
		t.Fatalf("target advanced after source branch switch: %s", got)
	}
	if got := gitTest(t, repo, "branch", "--show-current"); got != "same-head-external" {
		t.Fatalf("external source branch was not preserved: %s", got)
	}
}

func TestMergeBackFencesSourceHeadDuringRefTransaction(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	attempted := false
	mergeStepHook = func(step string) {
		if step != "before_merge_ref_transaction" {
			return
		}
		attempted = true
		gitTest(t, repo, "update-ref", "refs/heads/same-head-external", inspection.TargetHead)
		if _, _, switchErr := runGit(context.Background(), repo, "symbolic-ref", "HEAD", "refs/heads/same-head-external"); switchErr == nil {
			t.Fatal("source HEAD changed while the mutation fence was held")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err != nil || !result.Merged || result.RecoveryRequired {
		t.Fatalf("HEAD-fenced merge = %+v, %v", result, err)
	}
	if !attempted {
		t.Fatal("source branch switch was not attempted at the ref transaction boundary")
	}
	if got := gitTest(t, repo, "branch", "--show-current"); got != inspection.TargetBranch {
		t.Fatalf("source branch changed despite the HEAD fence: %s", got)
	}
}

func TestMergeBackRejectsIndexMutationBeforeRefUpdate(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	latePath := filepath.Join(repo, "late.txt")
	mergeStepHook = func(step string) {
		if step != "after_merge_commit_object" {
			return
		}
		if err := os.WriteFile(latePath, []byte("preserve me\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitTest(t, repo, "add", "late.txt")
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "source changed before target ref update") {
		t.Fatalf("index mutation = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+inspection.TargetBranch); got != inspection.TargetHead {
		t.Fatalf("target ref advanced to %s", got)
	}
	if body, readErr := os.ReadFile(latePath); readErr != nil || string(body) != "preserve me\n" {
		t.Fatalf("late content was not preserved: %q, %v", body, readErr)
	}
}

func TestMergeBackRefCompareAndSwapPreservesExternalAdvance(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	externalHead := ""
	mergeStepHook = func(step string) {
		if step != "before_merge_ref_update" {
			return
		}
		tree := gitTest(t, repo, "rev-parse", inspection.TargetHead+"^{tree}")
		externalHead = gitTest(t, repo, "commit-tree", tree, "-p", inspection.TargetHead, "-m", "external advance")
		gitTest(t, repo, "update-ref", "refs/heads/"+inspection.TargetBranch, externalHead, inspection.TargetHead)
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "compare-and-swap") {
		t.Fatalf("ref drift = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+inspection.TargetBranch); got != externalHead {
		t.Fatalf("external ref was overwritten: got %s, want %s", got, externalHead)
	}
}

func TestMergeBackPreservesChangedMergeHeadAfterRefUpdate(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	mergeStepHook = func(step string) {
		if step == "after_merge_ref_update" {
			mergeHeadPath := gitTest(t, repo, "rev-parse", "--git-path", "MERGE_HEAD")
			if !filepath.IsAbs(mergeHeadPath) {
				mergeHeadPath = filepath.Join(repo, mergeHeadPath)
			}
			if err := os.WriteFile(mergeHeadPath, []byte(inspection.TargetHead+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "MERGE_HEAD changed") {
		t.Fatalf("MERGE_HEAD drift = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "MERGE_HEAD"); got != inspection.TargetHead {
		t.Fatalf("changed MERGE_HEAD was removed: got %s", got)
	}
	parents := strings.Fields(gitTest(t, repo, "rev-list", "--parents", "-n", "1", "refs/heads/"+inspection.TargetBranch))
	if len(parents) != 3 || parents[1] != inspection.TargetHead || parents[2] != inspection.WorktreeHead {
		t.Fatalf("installed exact merge parents = %v", parents)
	}
}

func TestMergeBackRejectsWorktreeAdvanceBeforePrepare(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	externalHead := ""
	mergeStepHook = func(step string) {
		if step == "before_merge_prepare" {
			gitCommitFile(t, created.WorktreeRoot, "late.txt", "late\n", "late worktree commit")
			externalHead = gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "worktree changed before merge preparation") {
		t.Fatalf("worktree advance before prepare = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != inspection.TargetHead {
		t.Fatalf("target advanced to %s", got)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != externalHead {
		t.Fatalf("external worktree commit was not preserved: got %s, want %s", got, externalHead)
	}
}

func TestMergeBackRejectsWorktreeContentDriftBeforePrepare(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	latePath := filepath.Join(created.WorktreeRoot, "late.txt")
	mergeStepHook = func(step string) {
		if step == "before_merge_prepare" {
			if err := os.WriteFile(latePath, []byte("preserve me\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "contents changed") {
		t.Fatalf("worktree content drift before prepare = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != inspection.TargetHead {
		t.Fatalf("target advanced to %s", got)
	}
	if body, readErr := os.ReadFile(latePath); readErr != nil || string(body) != "preserve me\n" {
		t.Fatalf("late worktree content was not preserved: %q, %v", body, readErr)
	}
}

func TestMergeBackRejectsWorktreeBranchSwitchBeforePrepare(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	mergeStepHook = func(step string) {
		if step == "before_merge_prepare" {
			gitTest(t, created.WorktreeRoot, "switch", "-c", "external-worktree-branch")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "worktree branch") {
		t.Fatalf("worktree branch switch = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != inspection.TargetHead {
		t.Fatalf("target advanced to %s", got)
	}
	if got := gitTest(t, created.WorktreeRoot, "symbolic-ref", "--short", "HEAD"); got != "external-worktree-branch" {
		t.Fatalf("external worktree branch was not preserved: %s", got)
	}
}

func TestMergeBackRefTransactionRejectsWorktreeAdvanceAtomically(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	externalHead := ""
	mergeStepHook = func(step string) {
		if step == "before_merge_ref_transaction" {
			gitCommitFile(t, created.WorktreeRoot, "late.txt", "late\n", "late worktree commit")
			externalHead = gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || result.RecoveryRequired {
		t.Fatalf("atomic worktree ref drift = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != inspection.TargetHead {
		t.Fatalf("target transaction partially updated ref to %s", got)
	}
	if operation, operationErr := gitOperation(context.Background(), repo); operationErr != nil || operation != "" {
		t.Fatalf("source merge state was not aborted: operation=%q, err=%v", operation, operationErr)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != externalHead {
		t.Fatalf("external worktree commit was not preserved: got %s, want %s", got, externalHead)
	}
}

func TestMergeBackReportsRecoveryWhenWorktreeAdvancesAfterRefTransaction(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	externalHead := ""
	mergeStepHook = func(step string) {
		if step == "after_merge_ref_update" {
			gitCommitFile(t, created.WorktreeRoot, "late.txt", "late\n", "late worktree commit")
			externalHead = gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired || !strings.Contains(result.Error, "worktree identity changed") {
		t.Fatalf("post-CAS worktree drift = %+v, %v", result, err)
	}
	parents := strings.Fields(gitTest(t, repo, "rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 || parents[1] != inspection.TargetHead || parents[2] != inspection.WorktreeHead {
		t.Fatalf("installed merge parents = %v", parents)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != externalHead {
		t.Fatalf("post-CAS worktree commit was not preserved: got %s, want %s", got, externalHead)
	}
}
