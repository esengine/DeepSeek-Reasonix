package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestApplyToolPreservesPOSIXEntryIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode and same-byte link fixture")
	}
	for _, scenario := range []string{"candidate_mode", "live_mode_conflict", "candidate_type", "live_type_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			repo := gitRepo(t)
			identity := opened(t, repo)
			for _, key := range []string{"core.filemode", "core.symlinks"} {
				if out, err := identity.Command(t.Context(), "config", key, "true").CombinedOutput(); err != nil {
					t.Fatalf("configure fixture: %v %s", err, out)
				}
			}
			s := NewStore(testenv.TempDir(t), repo, identity)
			report, err := s.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
				write(t, run.WorkspaceRoot, "b.txt", "candidate sibling\n")
				path := filepath.Join(run.WorkspaceRoot, "a.txt")
				switch scenario {
				case "candidate_mode":
					return Outcome{}, os.Chmod(path, 0o755)
				case "candidate_type":
					if err := os.Remove(path); err != nil {
						return Outcome{}, err
					}
					return Outcome{}, os.Symlink("one\n", path)
				default:
					write(t, run.WorkspaceRoot, "a.txt", "candidate\n")
					return Outcome{}, nil
				}
			}, repo, "change entry", "")
			if err != nil {
				t.Fatal(err)
			}
			id := idOf(t, report)
			e, err := s.lookup(id)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(repo, "a.txt")
			if scenario == "live_mode_conflict" {
				if err := os.Chmod(source, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "live_type_conflict" {
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("one\n", source); err != nil {
					t.Fatal(err)
				}
			}
			_, err = NewApplyTool(s).Execute(t.Context(), args(id))
			if strings.HasPrefix(scenario, "live_") {
				var refusal tool.Refusal
				if !errors.As(err, &refusal) || refusal.Code != CodeApplyConflict {
					t.Fatalf("apply tool missed the live entry conflict: %v", err)
				}
				if !slices.Contains(s.Pending(), id) {
					t.Fatal("conflicted result lost its pending identity")
				}
				if _, err := os.Lstat(filepath.Join(e.Cand.WorkspaceRoot, "a.txt")); err != nil {
					t.Fatalf("conflicted candidate was deleted: %v", err)
				}
				if _, err := os.Stat(filepath.Join(repo, "b.txt")); !os.IsNotExist(err) {
					t.Fatalf("conflicted apply wrote a sibling: %v", err)
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
				t.Fatal("apply tool discarded the candidate's executable mode")
			}
			if scenario == "candidate_type" && info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("apply tool discarded the candidate's file type")
			}
			if slices.Contains(s.Pending(), id) {
				t.Fatal("successfully applied result remained pending")
			}
		})
	}
}
