package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestApplyPathsPreservesPOSIXEntryIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable modes and same-byte link target fixture; checkout emulation is tested separately")
	}
	for _, scenario := range []string{"candidate_mode", "live_mode_conflict", "candidate_type", "live_type_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			repo, snap, c, finish := isolatedCandidate(t)
			gitIn(t, repo, "config", "core.filemode", "true")
			gitIn(t, repo, "config", "core.symlinks", "true")
			source := filepath.Join(repo, "a.txt")
			candidate := filepath.Join(c.WorkspaceRoot, "a.txt")
			writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
			switch scenario {
			case "candidate_mode":
				if err := os.Chmod(candidate, 0o755); err != nil {
					t.Fatal(err)
				}
			case "candidate_type":
				replaceWithSameByteLink(t, candidate)
			default:
				writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate\n")
			}
			tree, changes := finish()
			if len(changes) != 2 {
				t.Fatalf("candidate did not contain both required changes: %+v", changes)
			}
			switch scenario {
			case "live_mode_conflict":
				if err := os.Chmod(source, 0o755); err != nil {
					t.Fatal(err)
				}
			case "live_type_conflict":
				replaceWithSameByteLink(t, source)
			}
			err := ApplyPaths(t.Context(), snap, tree, changes)
			if strings.HasPrefix(scenario, "live_") {
				if !errors.Is(err, ErrApplyConflict) {
					t.Fatalf("source mode/type change accepted instead of conflict: %v", err)
				}
				if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
					t.Fatalf("conflicting entry allowed a sibling write: %q", got)
				}
				if scenario == "live_type_conflict" {
					if target, err := os.Readlink(source); err != nil || target != "one\n" {
						t.Fatalf("refusal changed the source link: %q %v", target, err)
					}
				} else {
					info, err := os.Stat(source)
					if err != nil || info.Mode().Perm()&0o100 == 0 || readRepoFile(t, repo, "a.txt") != "one\n" {
						t.Fatalf("refusal changed source mode/content: %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(source)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "candidate_mode" && info.Mode().Perm()&0o100 == 0 {
				t.Fatal("apply reported success but discarded executable mode")
			}
			if scenario == "candidate_type" && info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("apply reported success but discarded file type")
			}
			if got := readRepoFile(t, repo, "b.txt"); got != "candidate b\n" {
				t.Fatalf("valid entry change did not apply sibling: %q", got)
			}
			if err := ApplyPaths(t.Context(), snap, tree, changes); err != nil {
				t.Fatalf("already-target entry was not idempotent: %v", err)
			}
		})
	}
}

func replaceWithSameByteLink(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("one\n", path); err != nil {
		t.Fatal(err)
	}
}
