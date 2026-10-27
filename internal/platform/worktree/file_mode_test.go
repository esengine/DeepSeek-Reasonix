package worktree

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestApplyPathsWithUntrustedModesPreservesTheIndex(t *testing.T) {
	for _, scenario := range []string{"content", "source_index_mode", "candidate_mode", "candidate_type"} {
		t.Run(scenario, func(t *testing.T) {
			repo, snap, c, finish := isolatedCandidate(t)
			gitIn(t, repo, "config", "core.autocrlf", "true")
			gitIn(t, repo, "config", "core.filemode", "false")
			gitIn(t, repo, "config", "core.symlinks", "false")
			writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
			writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
			if scenario == "candidate_mode" {
				gitIn(t, c.WorkspaceRoot, "update-index", "--chmod=+x", "a.txt")
			}
			if scenario == "candidate_type" {
				blob := strings.TrimSpace(gitIn(t, c.WorkspaceRoot, "hash-object", "-w", "--", "a.txt"))
				gitIn(t, c.WorkspaceRoot, "update-index", "--cacheinfo", "120000,"+blob+",a.txt")
			}
			tree, changes := finish()
			if scenario == "source_index_mode" {
				gitIn(t, repo, "update-index", "--chmod=+x", "a.txt")
			}
			index := filepath.Join(repo, ".git", "index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			err = ApplyPaths(t.Context(), snap, tree, changes)
			switch scenario {
			case "content":
				if err != nil || readRepoFile(t, repo, "a.txt") != "candidate\n" {
					t.Fatalf("ordinary content apply refused: %v", err)
				}
			case "source_index_mode":
				if !errors.Is(err, ErrApplyConflict) {
					t.Fatalf("source index's mode change was ignored: %v", err)
				}
			case "candidate_mode":
				if err == nil || !strings.Contains(err.Error(), "core.filemode") {
					t.Fatalf("unrepresentable target mode was reported applied: %v", err)
				}
			case "candidate_type":
				if err == nil || !strings.Contains(err.Error(), "core.symlinks") {
					t.Fatalf("unrepresentable target type was reported applied: %v", err)
				}
			}
			if scenario != "content" && readRepoFile(t, repo, "b.txt") != "two\n" {
				t.Fatal("mode refusal wrote a sibling file")
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("apply modified the user's index: %v", err)
			}
		})
	}
}

func TestApplyPathsUsesGitOwnerExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	repo, snap, c, finish := isolatedCandidate(t)
	gitIn(t, repo, "config", "core.filemode", "true")
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
	tree, changes := finish()
	if err := os.Chmod(filepath.Join(repo, "a.txt"), 0o645); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
		t.Fatalf("non-owner executable bits do not change Git file mode: %v", err)
	}
}

func TestApplyPathsEmulatedLinksUseSnapshotFilters(t *testing.T) {
	requireGit(t)
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "core.filemode", "false")
	gitIn(t, repo, "config", "core.symlinks", "false")
	writeRepoFile(t, repo, ".gitattributes", "a.txt text eol=lf\n")
	writeRepoFile(t, repo, "a.txt", "target\r\n")
	gitIn(t, repo, "add", ".gitattributes")
	blob := strings.TrimSpace(gitIn(t, repo, "hash-object", "-w", "--no-filters", "--", "a.txt"))
	gitIn(t, repo, "update-index", "--add", "--cacheinfo", "120000,"+blob+",a.txt")
	gitIn(t, repo, "commit", "-q", "-m", "emulated link")
	snap, err := TakeSnapshot(t.Context(), opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateCandidate(t.Context(), snap, testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveCandidate(context.WithoutCancel(t.Context()), snap, c) })
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "new-target\r\n")
	tree, changes, err := CandidateTree(t.Context(), snap, c)
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(repo, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
			t.Fatalf("emulated link apply/idempotence failed: %v", err)
		}
	}
	got, err := os.ReadFile(filepath.Join(repo, "a.txt"))
	if err != nil || string(got) != "new-target\n" {
		t.Fatalf("emulated link did not match Git's staged target: %q %v", got, err)
	}
	after, err := os.ReadFile(index)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("emulated-link apply modified source index: %v", err)
	}
}

func TestApplyPathsRefusesUnsupportedTreeEntriesBeforeWriting(t *testing.T) {
	repo, snap, c, _ := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate sibling\n")
	gitIn(t, c.WorkspaceRoot, "add", "-A")
	commit := strings.TrimSpace(gitIn(t, c.WorkspaceRoot, "rev-parse", "HEAD"))
	gitIn(t, c.WorkspaceRoot, "update-index", "--cacheinfo", "160000,"+commit+",a.txt")
	tree := strings.TrimSpace(gitIn(t, c.WorkspaceRoot, "write-tree"))
	changes, err := treeChanges(t.Context(), opened(t, repo), snap.Tree, tree)
	if err != nil || len(changes) != 2 {
		t.Fatalf("unsupported-entry fixture: %+v %v", changes, err)
	}
	err = ApplyPaths(t.Context(), snap, tree, changes)
	if err == nil || !strings.Contains(err.Error(), "unsupported candidate tree entry") {
		t.Fatalf("unsupported Git entry was treated as an absent file: %v", err)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
		t.Fatalf("unsupported entry allowed a sibling write: %q", got)
	}
}
