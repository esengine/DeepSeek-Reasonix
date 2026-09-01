package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mergedWorktreeFixture(t *testing.T) (string, string, Result, MergeResult) {
	t.Helper()
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err != nil {
		t.Fatalf("MergeBack: %v (%+v)", err, result)
	}
	return repo, managed, created, result
}

func TestFinalizeMergePreservesLateContentAtFormerPath(t *testing.T) {
	requireGit(t)
	_, managed, created, result := mergedWorktreeFixture(t)
	latePath := filepath.Join(created.WorktreeRoot, "late-user.txt")
	mergeStepHook = func(step string) {
		if step != "after_cleanup_quarantine" {
			return
		}
		if err := os.MkdirAll(created.WorktreeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(latePath, []byte("preserve me\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || !cleanup.Completed || !cleanup.WorktreeRemoved || !cleanup.BranchDeleted || !hasBlocker(cleanup.Blockers, "late_content_preserved") {
		t.Fatalf("late-path cleanup = %+v, %v", cleanup, err)
	}
	body, readErr := os.ReadFile(latePath)
	if readErr != nil || string(body) != "preserve me\n" {
		t.Fatalf("late path content was deleted: %q, %v", body, readErr)
	}
}

func TestFinalizeMergeRestoresQuarantineWhenContentAppears(t *testing.T) {
	requireGit(t)
	_, managed, created, result := mergedWorktreeFixture(t)
	mergeStepHook = func(step string) {
		if step != "after_cleanup_quarantine" {
			return
		}
		quarantineDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
		entries, err := os.ReadDir(quarantineDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("quarantine entries = %v, %v", entries, err)
		}
		if err := os.WriteFile(filepath.Join(quarantineDir, entries[0].Name(), "late.bin"), []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !strings.Contains(cleanup.Error, "cleanup_state_changed") {
		t.Fatalf("quarantine drift cleanup = %+v, %v", cleanup, err)
	}
	body, readErr := os.ReadFile(filepath.Join(created.WorktreeRoot, "late.bin"))
	if readErr != nil || string(body) != "keep\n" {
		t.Fatalf("quarantine drift was not restored: %q, %v", body, readErr)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != result.WorktreeHead {
		t.Fatalf("restored worktree HEAD = %s", got)
	}
}

func TestFinalizeMergeSecondGateRestoresLateQuarantineContent(t *testing.T) {
	requireGit(t)
	_, managed, created, result := mergedWorktreeFixture(t)
	mergeStepHook = func(step string) {
		if step != "before_cleanup_quarantine_remove" {
			return
		}
		quarantineDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
		entries, err := os.ReadDir(quarantineDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("quarantine entries = %v, %v", entries, err)
		}
		if err := os.WriteFile(filepath.Join(quarantineDir, entries[0].Name(), "last-moment.bin"), []byte("keep late\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !strings.Contains(cleanup.Error, "cleanup_state_changed") {
		t.Fatalf("second cleanup gate = %+v, %v", cleanup, err)
	}
	body, readErr := os.ReadFile(filepath.Join(created.WorktreeRoot, "last-moment.bin"))
	if readErr != nil || string(body) != "keep late\n" {
		t.Fatalf("last-moment quarantine content was not restored: %q, %v", body, readErr)
	}
}

func TestFinalizeMergeRetriesPreservedQuarantineWithOccupiedFormerPath(t *testing.T) {
	requireGit(t)
	_, managed, created, result := mergedWorktreeFixture(t)
	formerLatePath := filepath.Join(created.WorktreeRoot, "former-late.bin")
	quarantineLatePath := ""
	mergeStepHook = func(step string) {
		if step != "after_cleanup_quarantine" {
			return
		}
		quarantineDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
		entries, err := os.ReadDir(quarantineDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("quarantine entries = %v, %v", entries, err)
		}
		quarantineLatePath = filepath.Join(quarantineDir, entries[0].Name(), "quarantine-late.bin")
		if err := os.WriteFile(quarantineLatePath, []byte("remove before retry\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(created.WorktreeRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(formerLatePath, []byte("preserve forever\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !strings.Contains(cleanup.Error, "cleanup_state_changed") || quarantineLatePath == "" {
		t.Fatalf("first quarantined cleanup = %+v, %v", cleanup, err)
	}
	if removeErr := os.Remove(quarantineLatePath); removeErr != nil {
		t.Fatal(removeErr)
	}
	cleanup, err = FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || !cleanup.Completed || !cleanup.WorktreeRemoved || !cleanup.BranchDeleted || !hasBlocker(cleanup.Blockers, "late_content_preserved") {
		t.Fatalf("retried quarantined cleanup = %+v, %v", cleanup, err)
	}
	body, readErr := os.ReadFile(formerLatePath)
	if readErr != nil || string(body) != "preserve forever\n" {
		t.Fatalf("former-path content changed: %q, %v", body, readErr)
	}
}

func TestFinalizeMergeBranchDeleteUsesCompareAndSwap(t *testing.T) {
	requireGit(t)
	repo, managed, _, result := mergedWorktreeFixture(t)
	externalHead := ""
	mergeStepHook = func(step string) {
		if step != "before_cleanup_branch_delete" {
			return
		}
		tree := gitTest(t, repo, "rev-parse", result.WorktreeHead+"^{tree}")
		externalHead = gitTest(t, repo, "commit-tree", tree, "-p", result.WorktreeHead, "-m", "external branch advance")
		gitTest(t, repo, "update-ref", "refs/heads/"+result.WorktreeBranch, externalHead, result.WorktreeHead)
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !cleanup.WorktreeRemoved || cleanup.BranchDeleted {
		t.Fatalf("branch CAS cleanup = %+v, %v", cleanup, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != externalHead {
		t.Fatalf("external branch was deleted or overwritten: got %s, want %s", got, externalHead)
	}
}

func TestFinalizeMergePreservesWorktreeMovedOutsideQuarantine(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	externalRoot := filepath.Join(t.TempDir(), "externally-moved")
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, externalRoot)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || cleanup.WorktreeRemoved || cleanup.BranchDeleted || !strings.Contains(cleanup.Error, "unexpected path") {
		t.Fatalf("externally moved cleanup = %+v, %v", cleanup, err)
	}
	if got := gitTest(t, externalRoot, "rev-parse", "HEAD"); got != result.WorktreeHead {
		t.Fatalf("externally moved worktree HEAD = %s", got)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("externally moved branch changed to %s", got)
	}
}

func TestFinalizeMergeRetriesAfterWorktreeRemovalAndBranchLockFailure(t *testing.T) {
	requireGit(t)
	repo, managed, _, result := mergedWorktreeFixture(t)
	commonDir := gitTest(t, repo, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repo, commonDir)
	}
	lockPath := filepath.Join(commonDir, "refs", "heads", filepath.FromSlash(result.WorktreeBranch)+".lock")
	mergeStepHook = func(step string) {
		if step != "before_cleanup_branch_delete" {
			return
		}
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, []byte("held\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !cleanup.WorktreeRemoved || cleanup.BranchDeleted {
		t.Fatalf("branch-lock cleanup = %+v, %v", cleanup, err)
	}
	if removeErr := os.Remove(lockPath); removeErr != nil {
		t.Fatal(removeErr)
	}
	mergeStepHook = nil
	cleanup, err = FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || !cleanup.Completed || !cleanup.WorktreeRemoved || !cleanup.BranchDeleted {
		t.Fatalf("branch-lock retry = %+v, %v", cleanup, err)
	}
}
