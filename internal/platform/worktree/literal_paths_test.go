package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func literalCandidate(t *testing.T, name string, ignoreMode bool) (string, Snapshot, Candidate) {
	t.Helper()
	requireGit(t)
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	if ignoreMode {
		gitIn(t, repo, "config", "core.filemode", "false")
	}
	writeRepoFile(t, repo, "a.txt", "source baseline\n")
	writeRepoFile(t, repo, name, "literal baseline\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "literal files")
	snap, err := TakeSnapshot(t.Context(), opened(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateCandidate(t.Context(), snap, testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveCandidate(context.WithoutCancel(t.Context()), snap, c) })
	return repo, snap, c
}

func TestApplyPathsTreatsFilenamesLiterally(t *testing.T) {
	for _, name := range []string{"*.txt", "[a].txt", ":(top)a.txt", "-dash.txt", "space file.txt", "\"quoted.txt", "quote\"file.txt", "line\nfile.txt", "tab\tfile.txt", "slash\\file.txt", "工具.txt", "separator\u2028file.txt"} {
		for _, ignoreMode := range []bool{false, true} {
			t.Run(name+map[bool]string{true: "/index_mode", false: "/filesystem_mode"}[ignoreMode], func(t *testing.T) {
				if runtime.GOOS == "windows" && strings.ContainsAny(name, "*:\"\n\t\\") {
					t.Skip("literal filename is not representable on Windows")
				}
				repo, snap, c := literalCandidate(t, name, ignoreMode)
				writeRepoFile(t, c.WorkspaceRoot, name, "candidate literal\n")
				tree, changes, err := CandidateTree(t.Context(), snap, c)
				if err != nil || len(changes) != 1 || changes[0].Path != name {
					t.Fatalf("literal fixture changes = %+v, %v", changes, err)
				}
				writeRepoFile(t, repo, "a.txt", "unrelated user edit\n")
				for range 2 {
					if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
						t.Fatalf("literal apply/idempotence refused: %v", err)
					}
				}
				if got := readRepoFile(t, repo, name); got != "candidate literal\n" {
					t.Fatalf("literal file was not applied: %q", got)
				}
				if got := readRepoFile(t, repo, "a.txt"); got != "unrelated user edit\n" {
					t.Fatalf("restore wrote an undeclared source path: %q", got)
				}
			})
		}
	}
}

func TestLiteralPathConflictWritesNothing(t *testing.T) {
	for _, name := range []string{"[a].txt", "line\nfile.txt", "quote\"file.txt"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsAny(name, "\n\"") {
				t.Skip("literal filename is not representable on Windows")
			}
			repo, snap, c := literalCandidate(t, name, true)
			writeRepoFile(t, c.WorkspaceRoot, name, "candidate conflict\n")
			writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate sibling\n")
			tree, changes, err := CandidateTree(t.Context(), snap, c)
			if err != nil {
				t.Fatal(err)
			}
			writeRepoFile(t, repo, name, "user conflict\n")
			if err := ApplyPaths(t.Context(), snap, tree, changes); !errors.Is(err, ErrApplyConflict) {
				t.Fatalf("literal conflict was not attributed: %v", err)
			}
			if got := readRepoFile(t, repo, name); got != "user conflict\n" {
				t.Fatalf("literal conflict changed source: %q", got)
			}
			if _, err := os.Stat(filepath.Join(repo, "b.txt")); !os.IsNotExist(err) {
				t.Fatalf("conflict wrote a sibling: %v", err)
			}
		})
	}
}

func TestWholeWorkspaceApplyUsesLiteralFilenames(t *testing.T) {
	for _, name := range []string{"[a].txt", "*.txt"} {
		t.Run(name, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.Contains(name, "*") {
				t.Skip("wildcard filename is not representable on Windows")
			}
			repo, snap, c := literalCandidate(t, name, false)
			writeRepoFile(t, c.WorkspaceRoot, name, "whole apply literal\n")
			tree, changes, err := CandidateTree(t.Context(), snap, c)
			if err != nil {
				t.Fatal(err)
			}
			if err := Apply(t.Context(), snap, tree, changes); err != nil {
				t.Fatal(err)
			}
			if got := readRepoFile(t, repo, name); got != "whole apply literal\n" {
				t.Fatalf("whole-workspace apply missed literal: %q", got)
			}
			if got := readRepoFile(t, repo, "a.txt"); got != "source baseline\n" {
				t.Fatalf("whole-workspace apply changed another path: %q", got)
			}
		})
	}
}
