package worktree

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectMergeUsesRecordedLinkedSource(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	linked := filepath.Join(t.TempDir(), "source-linked")
	gitTest(t, repo, "worktree", "add", "-b", "feature/source", linked, "HEAD")
	managed := t.TempDir()
	created, err := Create(context.Background(), linked, managed)
	if err != nil {
		t.Fatal(err)
	}

	inspection, err := InspectMerge(context.Background(), created.WorkspaceRoot, managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := sameDirectory(inspection.SourceRoot, linked); err != nil || inspection.TargetBranch != "feature/source" {
		t.Fatalf("inspection source identity = %+v", inspection)
	}
	if inspection.ChangedFiles == nil || inspection.ConflictFiles == nil || inspection.Blockers == nil || inspection.CleanupBlockers == nil {
		t.Fatalf("inspection arrays must encode as []: %+v", inspection)
	}
}

func TestMergeBackRequiresExplicitDirtyCommitAndFinalizesSeparately(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorktreeRoot, "feature.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	request := requestFromInspection(inspection)
	if inspection.CanMerge || !inspection.WorktreeDirty {
		t.Fatalf("dirty inspection = %+v", inspection)
	}
	if result, err := MergeBack(context.Background(), managed, request); err == nil || result.Merged {
		t.Fatalf("dirty merge without opt-in = %+v, %v", result, err)
	}
	request.AutoCommitDirty = true
	result, err := MergeBack(context.Background(), managed, request)
	if err != nil {
		t.Fatalf("MergeBack: %v (%+v)", err, result)
	}
	if !result.Merged || result.MergedCommit == "" {
		t.Fatalf("merge result = %+v", result)
	}
	if _, err := os.Stat(created.WorktreeRoot); err != nil {
		t.Fatalf("merge removed worktree before finalize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.go")); err != nil {
		t.Fatalf("merged file missing from source: %v", err)
	}

	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil {
		t.Fatalf("FinalizeMerge: %v (%+v)", err, cleanup)
	}
	if cleanup.Completed || cleanup.WorktreeRemoved || cleanup.BranchDeleted || !cleanup.RecoveryRetained || !cleanup.RecoveryWorktreeRegistered || !cleanup.BranchRetained {
		t.Fatalf("cleanup result = %+v", cleanup)
	}
	if _, err := os.Stat(created.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("former worktree path remains after finalize: %v", err)
	}
	if _, err := os.Stat(cleanup.RecoveryRoot); err != nil {
		t.Fatalf("retained recovery worktree is missing: %v", err)
	}
}

func TestMergeBackRejectsTargetHeadDrift(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	gitCommitFile(t, repo, "source.txt", "source\n", "source drift")

	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !strings.Contains(result.Error, "identity changed") {
		t.Fatalf("drift merge = %+v, %v", result, err)
	}
	if _, err := os.Stat(created.WorktreeRoot); err != nil {
		t.Fatalf("drift removed recoverable worktree: %v", err)
	}
}

func TestMergeBackRejectsConfirmedWorktreeContentDrift(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(created.WorktreeRoot, "feature.txt")
	if err := os.WriteFile(path, []byte("confirmed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	if err := os.WriteFile(path, []byte("changed later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || !strings.Contains(result.Error, "identity changed") {
		t.Fatalf("content drift = %+v, %v", result, err)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != inspection.WorktreeHead {
		t.Fatalf("content drift created a commit: %s", got)
	}
}

func TestMergeBackPreservesStagingWhenContentChangesDuringAdd(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(created.WorktreeRoot, "feature.txt")
	if err := os.WriteFile(path, []byte("confirmed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	mergeStepHook = func(step string) {
		if step == "after_worktree_add" {
			if err := os.WriteFile(path, []byte("changed during add\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })
	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || !strings.Contains(result.Error, "real index was preserved") {
		t.Fatalf("add drift = %+v, %v", result, err)
	}
	status := gitTest(t, created.WorktreeRoot, "status", "--porcelain=v1")
	if !strings.Contains(status, "?? feature.txt") {
		t.Fatalf("real index or later worktree contents changed: %q", status)
	}
}

func TestMergeBackRejectsExtraCommitAfterAutoCommit(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorktreeRoot, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	mergeStepHook = func(step string) {
		if step == "after_worktree_commit" {
			gitCommitFile(t, created.WorktreeRoot, "extra.txt", "extra\n", "external extra")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })
	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || !strings.Contains(result.Error, "branch HEAD changed") {
		t.Fatalf("extra auto-commit = %+v, %v", result, err)
	}
}

func TestMergeBackStopsWhenSourceBranchChangesBeforePrepare(t *testing.T) {
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
			gitTest(t, repo, "switch", "-c", "source-drift")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })
	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "source changed before merge preparation") {
		t.Fatalf("source branch drift = %+v, %v", result, err)
	}
}

func TestMergeBackReportsRecoveryRequiredWhenPreparedSourceDrifts(t *testing.T) {
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
		if step == "after_merge_prepare" {
			// Preserve the prepared index and worktree while clearing merge
			// metadata so switching branches deterministically simulates source
			// identity drift on every supported Git platform.
			gitTest(t, repo, "merge", "--quit")
			gitTest(t, repo, "switch", "-c", "prepared-drift")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })
	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err == nil || result.Merged || !result.RecoveryRequired {
		t.Fatalf("prepared source drift = %+v, %v", result, err)
	}
}

func TestMergeBackRechecksConflictAfterDirtyCommit(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorktreeRoot, "README.md"), []byte("worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, repo, "README.md", "source\n", "source change")
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	sourceHead := gitTest(t, repo, "rev-parse", "HEAD")

	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || !strings.Contains(result.Error, "merge is blocked") {
		t.Fatalf("post-commit conflict = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != sourceHead {
		t.Fatalf("source HEAD changed on preflight conflict: %s != %s", got, sourceHead)
	}
	postCommit := inspectMergeTest(t, created.WorktreeRoot, managed)
	if postCommit.WorktreeDirty || !postCommit.HasConflicts {
		t.Fatalf("post-commit inspection = %+v", postCommit)
	}
}

func TestInspectMergeRejectsLegacyAndForgedMetadata(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	path := metadataPath(created.WorktreeRoot)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectMerge(context.Background(), created.WorkspaceRoot, managed)
	if err == nil || inspection.Available || !strings.Contains(inspection.Reason, "predates") {
		t.Fatalf("legacy inspection = %+v, %v", inspection, err)
	}

	forged := mergeMetadata{
		Version: mergeMetadataVersion, SourceRoot: repo, TargetBranch: gitTest(t, repo, "branch", "--show-current"),
		CreatedHead: created.Head, WorktreeRoot: repo, WorktreeBranch: created.Branch,
	}
	body, _ := json.Marshal(forged)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err = InspectMerge(context.Background(), created.WorkspaceRoot, managed)
	if err == nil || inspection.Available || !strings.Contains(inspection.Reason, "root mismatch") {
		t.Fatalf("forged inspection = %+v, %v", inspection, err)
	}
}

func TestInspectMergeBlocksSourceBranchAndDirtyState(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	if inspection.CanMerge || !hasBlocker(inspection.Blockers, "source_dirty") {
		t.Fatalf("source dirty inspection = %+v", inspection)
	}
	if err := os.Remove(filepath.Join(repo, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "switch", "-c", "other-target")
	inspection = inspectMergeTest(t, created.WorkspaceRoot, managed)
	if inspection.CanMerge || !hasBlocker(inspection.Blockers, "target_branch_drift") {
		t.Fatalf("source branch drift inspection = %+v", inspection)
	}
}

func TestFinalizeMergePreservesIgnoredContentAndSupportsRetry(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, created.WorktreeRoot, "feature.txt", "feature\n", "feature")
	inspection := inspectMergeTest(t, created.WorkspaceRoot, managed)
	result, err := MergeBack(context.Background(), managed, requestFromInspection(inspection))
	if err != nil {
		t.Fatal(err)
	}
	gitDir := gitTest(t, created.WorktreeRoot, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(created.WorktreeRoot, gitDir)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "info", "exclude"), []byte("cache.bin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.WorktreeRoot, "cache.bin"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanup, err := FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err == nil || cleanup.Completed || !hasBlocker(cleanup.Blockers, "worktree_content") {
		t.Fatalf("ignored cleanup = %+v, %v", cleanup, err)
	}
	if _, err := os.Stat(filepath.Join(created.WorktreeRoot, "cache.bin")); err != nil {
		t.Fatalf("ignored file was not preserved: %v", err)
	}
	if err := os.Remove(filepath.Join(created.WorktreeRoot, "cache.bin")); err != nil {
		t.Fatal(err)
	}
	cleanup, err = FinalizeMerge(context.Background(), managed, cleanupFromMerge(result))
	if err != nil || !cleanup.RecoveryRetained || !cleanup.RecoveryWorktreeRegistered || !cleanup.BranchRetained {
		t.Fatalf("cleanup retry = %+v, %v", cleanup, err)
	}
}

func TestInspectMergeRejectsOutsideManagedPath(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	inspection, err := InspectMerge(context.Background(), repo, t.TempDir())
	if err == nil || inspection.Available {
		t.Fatalf("outside managed inspection = %+v, %v", inspection, err)
	}
}

func inspectMergeTest(t *testing.T, workspaceRoot, managedRoot string) MergeInspection {
	t.Helper()
	inspection, err := InspectMerge(context.Background(), workspaceRoot, managedRoot)
	if err != nil {
		t.Fatalf("InspectMerge: %v (%+v)", err, inspection)
	}
	return inspection
}

func requestFromInspection(inspection MergeInspection) MergeRequest {
	return MergeRequest{
		WorkspaceRoot: inspection.WorktreeRoot, ExpectedTargetBranch: inspection.TargetBranch,
		ExpectedTargetHead: inspection.TargetHead, ExpectedWorktreeHead: inspection.WorktreeHead,
		ExpectedWorktreeStateToken: inspection.WorktreeStateToken,
	}
}

func cleanupFromMerge(result MergeResult) CleanupRequest {
	return CleanupRequest{
		WorktreeRoot: result.WorktreeRoot, SourceRoot: result.SourceRoot, TargetBranch: result.TargetBranch,
		MergedCommit: result.MergedCommit, WorktreeBranch: result.WorktreeBranch, WorktreeHead: result.WorktreeHead,
	}
}

func hasBlocker(blockers []MergeBlocker, code string) bool {
	for _, item := range blockers {
		if item.Code == code {
			return true
		}
	}
	return false
}

func gitCommitFile(t *testing.T, repo, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", name)
	gitTest(t, repo, "commit", "-m", message)
}

func gitTest(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Reasonix Test", "GIT_AUTHOR_EMAIL=reasonix@example.invalid",
		"GIT_COMMITTER_NAME=Reasonix Test", "GIT_COMMITTER_EMAIL=reasonix@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
