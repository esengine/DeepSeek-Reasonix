package worktree

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMergeBackRejectsSplitIndexWithoutLosingEitherVersion(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()
	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(created.WorktreeRoot, "README.md")
	if err := os.WriteFile(path, []byte("staged version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, created.WorktreeRoot, "add", "README.md")
	if err := os.WriteFile(path, []byte("working version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	_, indexBefore, err := snapshotRealIndex(context.Background(), created.WorktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true

	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "worktree_index_split") {
		t.Fatalf("split index merge = %+v, %v", result, err)
	}
	if got := gitTest(t, created.WorktreeRoot, "show", ":README.md"); got != "staged version" {
		t.Fatalf("staged version changed: %q", got)
	}
	body, readErr := os.ReadFile(path)
	if readErr != nil || string(body) != "working version\n" {
		t.Fatalf("working version changed: %q, %v", body, readErr)
	}
	if got := gitTest(t, created.WorktreeRoot, "rev-parse", "HEAD"); got != inspection.WorktreeHead {
		t.Fatalf("split index advanced HEAD to %s", got)
	}
	_, indexAfter, err := snapshotRealIndex(context.Background(), created.WorktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("split-index rejection changed the real index bytes")
	}
}

func TestMergeBackAutoCommitUsesExactTreeWithoutHooks(t *testing.T) {
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
	featurePath := filepath.Join(created.WorktreeRoot, "feature.txt")
	if err := os.WriteFile(featurePath, []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hookPath := gitTest(t, created.WorktreeRoot, "rev-parse", "--git-path", "hooks/pre-commit")
	if !filepath.IsAbs(hookPath) {
		hookPath = filepath.Join(created.WorktreeRoot, hookPath)
	}
	marker := filepath.Join(t.TempDir(), "hook-ran")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf ran > '"+marker+"'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	tempIndexSecured := false
	mergeStepHook = func(step string) {
		if step != "after_worktree_add" {
			return
		}
		matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(created.WorktreeRoot), ".reasonix-merge-index-*"))
		if globErr != nil || len(matches) != 1 {
			t.Fatalf("temporary index matches = %v, %v", matches, globErr)
		}
		info, statErr := os.Stat(matches[0])
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("temporary index mode = %v, %v", info.Mode().Perm(), statErr)
		}
		tempIndexSecured = true
	}
	t.Cleanup(func() { mergeStepHook = nil })
	result, err := MergeBack(context.Background(), managed, request)
	if err != nil || !result.Merged {
		t.Fatalf("hook-free auto-commit = %+v, %v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pre-commit hook ran: %v", err)
	}
	if !tempIndexSecured {
		t.Fatal("temporary index permissions were not verified")
	}
	parents := strings.Fields(gitTest(t, created.WorktreeRoot, "rev-list", "--parents", "-n", "1", result.WorktreeHead))
	if len(parents) != 2 || parents[1] != inspection.WorktreeHead {
		t.Fatalf("auto-commit parents = %v", parents)
	}
	if got := gitTest(t, created.WorktreeRoot, "show", result.WorktreeHead+":feature.txt"); got != "feature" {
		t.Fatalf("auto-commit tree content = %q", got)
	}
}

func TestMergeBackAutoCommitRejectsSameHeadBranchSwitchBeforeCAS(t *testing.T) {
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
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	mergeStepHook = func(step string) {
		if step == "before_worktree_ref_transaction" {
			gitTest(t, created.WorktreeRoot, "switch", "-c", "external-same-head")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || result.RecoveryRequired || !strings.Contains(result.Error, "worktree branch") {
		t.Fatalf("same-head branch switch = %+v, %v", result, err)
	}
	if got := gitTest(t, repo, "rev-parse", "refs/heads/"+inspection.WorktreeBranch); got != inspection.WorktreeHead {
		t.Fatalf("delivery branch advanced to %s", got)
	}
	if got := gitTest(t, created.WorktreeRoot, "symbolic-ref", "--short", "HEAD"); got != "external-same-head" {
		t.Fatalf("external branch switch was overwritten: %s", got)
	}
}

func TestMergeBackAutoCommitPreservesIndexMutationAfterRefCAS(t *testing.T) {
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
	inspection := inspectMergeTest(t, created.WorktreeRoot, managed)
	request := requestFromInspection(inspection)
	request.AutoCommitDirty = true
	latePath := filepath.Join(created.WorktreeRoot, "late-index.txt")
	mergeStepHook = func(step string) {
		if step == "after_worktree_ref_update" {
			if err := os.WriteFile(latePath, []byte("preserve\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitTest(t, created.WorktreeRoot, "add", "late-index.txt")
		}
	}
	t.Cleanup(func() { mergeStepHook = nil })

	result, err := MergeBack(context.Background(), managed, request)
	if err == nil || result.Merged || !result.RecoveryRequired {
		t.Fatalf("post-CAS index mutation = %+v, %v", result, err)
	}
	if got := gitTest(t, created.WorktreeRoot, "show", ":late-index.txt"); got != "preserve" {
		t.Fatalf("late staged content was overwritten: %q", got)
	}
	if got := gitTest(t, repo, "rev-parse", "HEAD"); got != inspection.TargetHead {
		t.Fatalf("source target advanced after auto-commit recovery: %s", got)
	}
}
