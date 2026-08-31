package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAndMergeBackWorktree(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	managed := t.TempDir()

	created, err := Create(context.Background(), repo, managed)
	if err != nil {
		t.Fatalf("Create worktree failed: %v", err)
	}

	// Make changes in worktree
	newFile := filepath.Join(created.WorkspaceRoot, "feature.go")
	if err := os.WriteFile(newFile, []byte("package feature\n\nfunc Hello() string { return \"hello\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Test InspectMerge when dirty
	insp, err := InspectMerge(context.Background(), created.WorkspaceRoot)
	if err != nil {
		t.Fatalf("InspectMerge failed: %v", err)
	}
	if !insp.Available {
		t.Fatalf("expected insp.Available=true, got reason: %s", insp.Reason)
	}
	if !insp.WorktreeDirty {
		t.Fatal("expected insp.WorktreeDirty=true")
	}
	if insp.FilesChanged < 1 {
		t.Fatalf("expected FilesChanged >= 1, got %d", insp.FilesChanged)
	}

	// Test MergeBack with AutoCommitDirty and cleanup
	res, err := MergeBack(context.Background(), created.WorkspaceRoot, MergeOptions{
		AutoCommitDirty: true,
		RemoveWorktree:  true,
		DeleteBranch:    true,
	})
	if err != nil {
		t.Fatalf("MergeBack failed: %v", err)
	}
	if !res.Merged {
		t.Fatalf("expected res.Merged=true, got error=%q", res.Error)
	}
	if !res.WorktreeRemoved {
		t.Fatal("expected worktree to be removed")
	}

	// Verify file is now in base repository
	mergedFile := filepath.Join(repo, "feature.go")
	content, err := os.ReadFile(mergedFile)
	if err != nil {
		t.Fatalf("expected feature.go in main repo: %v", err)
	}
	if !strings.Contains(string(content), "Hello()") {
		t.Fatalf("unexpected content in merged file: %s", content)
	}
}

func TestInspectMergeRejectsNonWorktree(t *testing.T) {
	insp, err := InspectMerge(context.Background(), "")
	if err == nil || insp.Available {
		t.Fatalf("expected error on empty path, got %+v", insp)
	}
}
