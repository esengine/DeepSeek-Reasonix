package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func assertRetainedCleanup(t *testing.T, repo string, result MergeResult, cleanup CleanupResult) {
	t.Helper()
	if cleanup.Completed || cleanup.WorktreeRemoved || cleanup.BranchDeleted ||
		!cleanup.RecoveryRetained || !cleanup.RecoveryWorktreeRegistered || !cleanup.BranchRetained || cleanup.RecoveryRoot == "" {
		t.Fatalf("cleanup did not retain recovery resources: %+v", cleanup)
	}
	if got := gitTest(t, cleanup.RecoveryRoot, "rev-parse", "HEAD"); got != result.WorktreeHead {
		t.Fatalf("recovery HEAD = %s, want %s", got, result.WorktreeHead)
	}
	if got := gitTest(t, cleanup.RecoveryRoot, "symbolic-ref", "--short", "HEAD"); got != result.WorktreeBranch {
		t.Fatalf("recovery branch = %s, want %s", got, result.WorktreeBranch)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("retained branch = %s, want %s", got, result.WorktreeHead)
	}
}

func TestFinalizeMergeRetainsRegisteredRecoveryWorktreeAndBranch(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	request := cleanupFromMerge(result)
	cleanup, err := FinalizeMerge(context.Background(), managed, request)
	if err != nil {
		t.Fatalf("FinalizeMerge: %v (%+v)", err, cleanup)
	}
	assertRetainedCleanup(t, repo, result, cleanup)
	if _, err := os.Stat(created.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("former worktree path still exists: %v", err)
	}
	journal, hasState, err := readCleanupState(mergeMetadata{WorktreeRoot: created.WorktreeRoot, WorktreeBranch: result.WorktreeBranch}, result.WorktreeHead)
	if err != nil || !hasState || journal.Current == nil || journal.Current.Stage != cleanupStageRetained {
		t.Fatalf("retained journal = %+v, %v, %v", journal, hasState, err)
	}
	retried, err := FinalizeMerge(context.Background(), managed, request)
	if err != nil || retried.RecoveryRoot != cleanup.RecoveryRoot {
		t.Fatalf("idempotent finalize = %+v, %v", retried, err)
	}
	assertRetainedCleanup(t, repo, result, retried)
	other := filepath.Join(t.TempDir(), "other")
	if _, _, err := runGit(context.Background(), repo, "worktree", "add", other, result.WorktreeBranch); err == nil {
		t.Fatal("retained branch could be checked out in a second worktree")
	}
}

func TestFinalizeMergePreservesOpenFileWritesAtRecoveryPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows may refuse to move a checkout containing an open file")
	}
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	openFile, err := os.OpenFile(filepath.Join(created.WorktreeRoot, "feature.txt"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = openFile.Close() })
	mergeStepHook = func(step string) {
		if step == "after_cleanup_recovery_move" {
			if _, err := openFile.WriteString("late write\n"); err != nil {
				t.Fatal(err)
			}
			if err := openFile.Sync(); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || !cleanup.RecoveryRetained || !cleanup.RecoveryWorktreeRegistered || !cleanup.BranchRetained ||
		!hasBlocker(cleanup.Blockers, "late_content_preserved") || !strings.Contains(cleanup.Error, "cleanup_state_changed") {
		t.Fatalf("open-file cleanup = %+v, %v", cleanup, err)
	}
	body, readErr := os.ReadFile(filepath.Join(cleanup.RecoveryRoot, "feature.txt"))
	if readErr != nil || string(body) != "feature\nlate write\n" {
		t.Fatalf("open-file write was lost: %q, %v", body, readErr)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("recovery branch changed after open-file write: %s", got)
	}
}

func TestFinalizeMergePreservesLateContentAtFormerPath(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	latePath := filepath.Join(created.WorktreeRoot, "late-user.txt")
	mergeStepHook = func(step string) {
		if step != "after_cleanup_recovery_move" {
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
	if err != nil || !hasBlocker(cleanup.Blockers, "late_content_preserved") {
		t.Fatalf("late-path cleanup = %+v, %v", cleanup, err)
	}
	assertRetainedCleanup(t, repo, result, cleanup)
	body, readErr := os.ReadFile(latePath)
	if readErr != nil || string(body) != "preserve me\n" {
		t.Fatalf("late path content was not preserved: %q, %v", body, readErr)
	}
	retried, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || retried.RecoveryRoot != cleanup.RecoveryRoot || !hasBlocker(retried.Blockers, "late_content_preserved") {
		t.Fatalf("late-path retry = %+v, %v", retried, err)
	}
}

func TestFinalizeMergeMoveFailureLeavesOriginalAndBranch(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	mergeStepHook = func(step string) {
		if step != "before_cleanup_recovery_move" {
			return
		}
		journal, hasState, err := readCleanupState(mergeMetadata{WorktreeRoot: created.WorktreeRoot, WorktreeBranch: result.WorktreeBranch}, result.WorktreeHead)
		if err != nil || !hasState || journal.Current == nil {
			t.Fatalf("planned cleanup journal = %+v, %v, %v", journal, hasState, err)
		}
		if err := os.WriteFile(journal.Current.RecoveryRoot, []byte("occupied\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.RecoveryRetained || cleanup.WorktreeRemoved || cleanup.BranchDeleted {
		t.Fatalf("move failure = %+v, %v", cleanup, err)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != result.WorktreeHead {
		t.Fatalf("original worktree changed after move failure: %s", got)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("branch changed after move failure: %s", got)
	}
}

func TestFinalizeMergeResumesPlannedRecoveryMove(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	metadata, _, _, err := readMergeMetadataForCleanup(created.WorktreeRoot, managed)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
	if err := ensureCleanupRecoveryDir(cleanupDir); err != nil {
		t.Fatal(err)
	}
	state := cleanupState{
		Version: cleanupStateVersion, OriginalRoot: created.WorktreeRoot,
		RecoveryRoot: filepath.Join(cleanupDir, "recovery-crash"), WorktreeBranch: result.WorktreeBranch,
		WorktreeHead: result.WorktreeHead, Stage: cleanupStagePlanned,
	}
	if err := createCleanupState(metadata, state); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, state.RecoveryRoot)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || cleanup.RecoveryRoot != state.RecoveryRoot {
		t.Fatalf("planned recovery retry = %+v, %v", cleanup, err)
	}
	assertRetainedCleanup(t, repo, result, cleanup)
	journal, hasState, err := readCleanupState(metadata, result.WorktreeHead)
	if err != nil || !hasState || journal.Current == nil || journal.Current.Stage != cleanupStageRetained {
		t.Fatalf("resumed cleanup journal = %+v, %v, %v", journal, hasState, err)
	}
}

func TestFinalizeMergeMigratesRegisteredLegacyJournal(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	metadata, _, _, err := readMergeMetadataForCleanup(created.WorktreeRoot, managed)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
	if err := ensureCleanupRecoveryDir(cleanupDir); err != nil {
		t.Fatal(err)
	}
	registeredRoot := filepath.Join(cleanupDir, "legacy-registered")
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, registeredRoot)
	manifest, err := captureCleanupManifest(context.Background(), registeredRoot)
	if err != nil {
		t.Fatal(err)
	}
	legacy := legacyCleanupState{
		Version: legacyCleanupStateVersion, OriginalRoot: created.WorktreeRoot, RegisteredRoot: registeredRoot,
		DetachedRoot: filepath.Join(cleanupDir, "legacy-detached"), WorktreeBranch: result.WorktreeBranch,
		WorktreeHead: result.WorktreeHead, Stage: legacyCleanupStagePrepared, Manifest: manifest,
	}
	writeLegacyCleanupState(t, metadata, legacy)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || cleanup.RecoveryRoot != registeredRoot {
		t.Fatalf("legacy migration = %+v, %v", cleanup, err)
	}
	assertRetainedCleanup(t, repo, result, cleanup)
	journal, hasState, err := readCleanupState(metadata, result.WorktreeHead)
	if err != nil || !hasState || journal.Current == nil || journal.Current.Version != cleanupStateVersion {
		t.Fatalf("migrated journal = %+v, %v, %v", journal, hasState, err)
	}
}

func TestFinalizeMergeRestoresPreparedLegacyDetachedCheckout(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	metadata, _, _, err := readMergeMetadataForCleanup(created.WorktreeRoot, managed)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
	if err := ensureCleanupRecoveryDir(cleanupDir); err != nil {
		t.Fatal(err)
	}
	registeredRoot := filepath.Join(cleanupDir, "legacy-registered")
	detachedRoot := filepath.Join(cleanupDir, "legacy-detached")
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, registeredRoot)
	manifest, err := captureCleanupManifest(context.Background(), registeredRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(registeredRoot, detachedRoot); err != nil {
		t.Fatal(err)
	}
	legacy := legacyCleanupState{
		Version: legacyCleanupStateVersion, OriginalRoot: created.WorktreeRoot, RegisteredRoot: registeredRoot,
		DetachedRoot: detachedRoot, WorktreeBranch: result.WorktreeBranch, WorktreeHead: result.WorktreeHead,
		Stage: legacyCleanupStagePrepared, Manifest: manifest,
	}
	writeLegacyCleanupState(t, metadata, legacy)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || cleanup.RecoveryRoot != registeredRoot {
		t.Fatalf("legacy detached restore = %+v, %v", cleanup, err)
	}
	assertRetainedCleanup(t, repo, result, cleanup)
}

func TestFinalizeMergePreservesUnregisteredLegacyDetachedCheckout(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	metadata, _, _, err := readMergeMetadataForCleanup(created.WorktreeRoot, managed)
	if err != nil {
		t.Fatal(err)
	}
	cleanupDir := filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-cleanup")
	if err := ensureCleanupRecoveryDir(cleanupDir); err != nil {
		t.Fatal(err)
	}
	registeredRoot := filepath.Join(cleanupDir, "legacy-registered")
	detachedRoot := filepath.Join(cleanupDir, "legacy-detached")
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, registeredRoot)
	manifest, err := captureCleanupManifest(context.Background(), registeredRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(registeredRoot, detachedRoot); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "worktree", "remove", registeredRoot)
	legacy := legacyCleanupState{
		Version: legacyCleanupStateVersion, OriginalRoot: created.WorktreeRoot, RegisteredRoot: registeredRoot,
		DetachedRoot: detachedRoot, WorktreeBranch: result.WorktreeBranch, WorktreeHead: result.WorktreeHead,
		Stage: legacyCleanupStageUnregistered, Manifest: manifest,
	}
	writeLegacyCleanupState(t, metadata, legacy)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.RecoveryRoot != detachedRoot || cleanup.RecoveryWorktreeRegistered ||
		!cleanup.BranchRetained || !hasBlocker(cleanup.Blockers, "legacy_recovery_preserved") {
		t.Fatalf("unregistered legacy recovery = %+v, %v", cleanup, err)
	}
	if _, err := os.Stat(detachedRoot); err != nil {
		t.Fatalf("legacy detached checkout was removed: %v", err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("legacy branch changed: %s", got)
	}
}

func TestFinalizeMergePreservesWorktreeMovedOutsideRecovery(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	externalRoot := filepath.Join(t.TempDir(), "externally-moved")
	gitTest(t, repo, "worktree", "move", created.WorktreeRoot, externalRoot)

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.RecoveryRetained || cleanup.WorktreeRemoved || cleanup.BranchDeleted || !strings.Contains(cleanup.Error, "unexpected path") {
		t.Fatalf("externally moved cleanup = %+v, %v", cleanup, err)
	}
	if got := gitTest(t, externalRoot, "rev-parse", "HEAD"); got != result.WorktreeHead {
		t.Fatalf("externally moved worktree HEAD = %s", got)
	}
}

func TestFinalizeMergeRejectsUnknownCleanupJournalVersion(t *testing.T) {
	requireGit(t)
	repo, managed, created, result := mergedWorktreeFixture(t)
	journal := cleanupJournalPath(mergeMetadata{WorktreeRoot: created.WorktreeRoot})
	if err := os.WriteFile(journal, []byte("{\"version\":99}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.RecoveryRetained || cleanup.WorktreeRemoved || cleanup.BranchDeleted || !strings.Contains(cleanup.Error, "unsupported cleanup state version") {
		t.Fatalf("unknown cleanup journal = %+v, %v", cleanup, err)
	}
	if _, err := os.Stat(created.WorktreeRoot); err != nil {
		t.Fatalf("unknown journal removed worktree: %v", err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+result.WorktreeBranch); got != result.WorktreeHead {
		t.Fatalf("unknown journal changed recovery branch: %s", got)
	}
}

func writeLegacyCleanupState(t *testing.T, metadata mergeMetadata, state legacyCleanupState) {
	t.Helper()
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cleanupJournalPath(metadata), append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
