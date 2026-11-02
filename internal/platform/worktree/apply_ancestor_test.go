package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestApplyPathsRejectsRedirectedAncestors(t *testing.T) {
	for _, kind := range []string{"outside_delete", "inside_delete", "dangling_delete", "outside_write", "inside_write", "regular_parent", "unchanged", "missing_creation"} {
		t.Run(kind, func(t *testing.T) {
			repo := initRepo(t)
			writeRepoFile(t, repo, "app/nested/victim.txt", "original\n")
			writeRepoFile(t, repo, "app/first.txt", "sibling\n")
			gitIn(t, repo, "add", "-A")
			gitIn(t, repo, "commit", "-q", "-m", "add project")
			snap, err := TakeSnapshot(t.Context(), opened(t, filepath.Join(repo, "app")))
			if err != nil {
				t.Fatal(err)
			}
			c, err := CreateCandidate(t.Context(), snap, filepath.Join(testenv.TempDir(t), "candidates"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = RemoveCandidate(context.Background(), snap, c) })
			if err := os.Remove(filepath.Join(c.WorkspaceRoot, "first.txt")); err != nil {
				t.Fatal(err)
			}
			writeTarget := kind == "outside_write" || kind == "inside_write"
			if writeTarget {
				writeRepoFile(t, c.WorkspaceRoot, "nested/victim.txt", "candidate\n")
			} else if err := os.Remove(filepath.Join(c.WorkspaceRoot, "nested/victim.txt")); err != nil {
				t.Fatal(err)
			}
			if kind == "missing_creation" {
				writeRepoFile(t, c.WorkspaceRoot, "new/deep/created.txt", "created\n")
			}
			tree, changes, err := CandidateTree(t.Context(), snap, c)
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) < 2 {
				t.Fatalf("missing candidate changes: %+v", changes)
			}
			outside := testenv.TempDir(t)
			if kind == "inside_delete" || kind == "inside_write" {
				outside = filepath.Join(repo, "elsewhere")
			}
			writeRepoFile(t, outside, "victim.txt", "original\n")
			parent := filepath.Join(repo, "app", "nested")
			control := kind == "unchanged" || kind == "missing_creation"
			if !control {
				if err := os.Rename(parent, filepath.Join(repo, "saved")); err != nil {
					t.Fatal(err)
				}
				if kind == "regular_parent" {
					if err := os.WriteFile(parent, []byte("not a directory\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else {
					target := outside
					if kind == "dangling_delete" {
						target = filepath.Join(outside, "missing")
					}
					linkApplyDirectory(t, target, parent)
				}
			}
			err = ApplyPaths(t.Context(), snap, tree, changes)
			if got := readRepoFile(t, outside, "victim.txt"); got != "original\n" {
				t.Fatalf("outside content changed: %q", got)
			}
			if control {
				if err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{"app/first.txt", "app/nested/victim.txt"} {
					if _, err := os.Lstat(filepath.Join(repo, path)); !os.IsNotExist(err) {
						t.Fatalf("deletion %s: %v", path, err)
					}
				}
				if kind == "missing_creation" && readRepoFile(t, repo, "app/new/deep/created.txt") != "created\n" {
					t.Fatal("missing parent creation failed")
				}
				return
			}
			var conflict *ConflictError
			if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != "app/nested/victim.txt" {
				t.Fatalf("ancestor conflict = %v", err)
			}
			if got := readRepoFile(t, repo, "app/first.txt"); got != "sibling\n" {
				t.Fatalf("refusal wrote sibling: %q", got)
			}
		})
	}
}

func TestWriteChangesRejectsRedirectedDeletion(t *testing.T) {
	repo := initRepo(t)
	outside := testenv.TempDir(t)
	writeRepoFile(t, outside, "victim.txt", "original\n")
	linkApplyDirectory(t, outside, filepath.Join(repo, "nested"))
	err := writeChanges(t.Context(), opened(t, repo), "HEAD", []Change{{Status: "D", Path: "nested/victim.txt"}})
	if err == nil {
		t.Fatal("publication accepted a redirected deletion")
	}
	if got := readRepoFile(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("outside file changed: %q", got)
	}
}

func TestWriteChangesUnlinksLeafDirectoryLink(t *testing.T) {
	repo := initRepo(t)
	outside := testenv.TempDir(t)
	writeRepoFile(t, outside, "victim.txt", "original\n")
	link := filepath.Join(repo, "link")
	linkApplyDirectory(t, outside, link)
	if err := writeChanges(t.Context(), opened(t, repo), "HEAD", []Change{{Status: "D", Path: "link"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("leaf link was not removed: %v", err)
	}
	if got := readRepoFile(t, outside, "victim.txt"); got != "original\n" {
		t.Fatalf("leaf deletion followed the link: %q", got)
	}
}

func TestWholeWorkspaceApplyPreservesFileToDirectoryTransition(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	if err := os.Remove(filepath.Join(c.WorkspaceRoot, "a.txt")); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, c.WorkspaceRoot, "a.txt/child.txt", "converted\n")
	tree, changes := finish()
	if err := Apply(t.Context(), snap, tree, changes); err != nil {
		t.Fatal(err)
	}
	if got := readRepoFile(t, repo, "a.txt/child.txt"); got != "converted\n" {
		t.Fatalf("conversion = %q", got)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
		t.Fatalf("sibling = %q", got)
	}
}

func linkApplyDirectory(t *testing.T, target, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput()
		if err != nil {
			t.Fatalf("create directory junction: %v %s", err, out)
		}
	} else if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
