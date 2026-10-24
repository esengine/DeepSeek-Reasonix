package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotMetadataPreservesCapturedModesAndSurvivesGC(t *testing.T) {
	repo, _, _, _ := isolatedCandidate(t)
	writeRepoFile(t, repo, "private/file", "private input\n")
	if err := os.Chmod(filepath.Join(repo, "private"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo, "private/file"), 0600); err != nil {
		t.Fatal(err)
	}
	initial, err := os.Stat(filepath.Join(repo, "private/file"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := TakeSnapshot(t.Context(), opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Metadata == "" {
		t.Fatal("snapshot omitted mode metadata")
	}
	if err := os.Chmod(filepath.Join(repo, "private/file"), 0644); err != nil {
		t.Fatal(err)
	}
	candidate, err := CreateCandidate(t.Context(), snapshot, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveCandidate(context.Background(), snapshot, candidate) })
	actual, err := os.Stat(filepath.Join(candidate.WorktreeRoot, "private/file"))
	if err != nil || initial.Mode().Perm() != actual.Mode().Perm() {
		t.Fatalf("candidate used live source or checkout mode: expected=%v actual=%v err=%v", initial.Mode(), actual, err)
	}
	sourceDir, err := os.Stat(filepath.Join(repo, "private"))
	if err != nil {
		t.Fatal(err)
	}
	candidateDir, err := os.Stat(filepath.Join(candidate.WorktreeRoot, "private"))
	if err != nil || candidateDir.Mode().Perm() != sourceDir.Mode().Perm() {
		t.Fatalf("candidate directory lost permissions: %v %v", candidateDir, err)
	}
	gitIn(t, repo, "gc", "--prune=now")
	if got := gitIn(t, repo, "cat-file", "-t", snapshot.Metadata); got != "blob\n" {
		t.Fatalf("live candidate metadata was not anchored: %q", got)
	}
}

func TestSnapshotMetadataRejectsMissingOrUnknownCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}
	entries := []snapshotEntry{{Path: "file", Mode: "100644"}}
	for name, metadata := range map[string]snapshotMetadata{
		"missing all":  {Version: 1},
		"missing root": {Version: 1, Files: map[string]uint32{"file": 0600}},
		"missing file": {Version: 1, Directories: map[string]uint32{".": 0700}},
		"unknown path": {Version: 1, Files: map[string]uint32{"elsewhere": 0600}, Directories: map[string]uint32{".": 0700}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := metadata.apply(root, entries); err == nil {
				t.Fatal("accepted incomplete or unrelated filesystem metadata")
			}
		})
	}
}

func TestSnapshotMetadataRejectsOversizedObjectBeforeReadingBlob(t *testing.T) {
	repo, snapshot, candidate, _ := isolatedCandidate(t)
	oid, _, err := runGitInput(t.Context(), opened(t, repo), strings.Repeat("x", snapshotMetadataMaxBytes+1), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Metadata = strings.TrimSpace(oid)
	if err := applySnapshotMetadata(t.Context(), snapshot, candidate); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized metadata was not rejected at size inspection: %v", err)
	}
}
