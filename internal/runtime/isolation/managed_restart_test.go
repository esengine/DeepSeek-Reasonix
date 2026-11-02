package isolation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestManagedCandidateSurvivesActualProcessExit(t *testing.T) {
	if mode := os.Getenv("REASONIX_TEST_CANDIDATE_EXIT"); mode != "" {
		root, managed := os.Getenv("REASONIX_TEST_CANDIDATE_SOURCE"), os.Getenv("REASONIX_TEST_CANDIDATE_MANAGED")
		s := NewStore(managed, root, opened(t, root))
		p, err := s.Prepare(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		run, err := p.BeginManaged(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(managed, "original-id"), []byte(run.ID()), 0o600); err != nil {
			t.Fatal(err)
		}
		if mode != "prepared" {
			if err := run.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			write(t, run.WorkspaceRoot(), "a.txt", "process-owned result\n")
		}
		if mode == "ready" || mode == "cleanup" {
			if err := run.Complete(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		if mode == "cleanup" {
			if out, err := s.repo.Command(t.Context(), "worktree", "lock", run.WorkspaceRoot()).CombinedOutput(); err != nil {
				t.Fatalf("lock: %v %s", err, out)
			}
			if _, err := s.Apply(t.Context(), run.ID()); !errors.Is(err, ErrCandidateCleanupPending) {
				t.Fatal(err)
			}
		}
		// Exit without defers: the next process must use the persisted original
		// ID/state, not in-memory entries or a fabricated new adoption identity.
		os.Exit(23)
	}
	for _, mode := range []string{"prepared", "running", "ready", "cleanup"} {
		t.Run(mode, func(t *testing.T) {
			root, managed := gitRepo(t), t.TempDir()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-test.run=^TestManagedCandidateSurvivesActualProcessExit$")
			cmd.Env = append(os.Environ(), "REASONIX_TEST_CANDIDATE_EXIT="+mode, "REASONIX_TEST_CANDIDATE_SOURCE="+root, "REASONIX_TEST_CANDIDATE_MANAGED="+managed)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("child: %v %s", err, out)
			}
			raw, err := os.ReadFile(filepath.Join(managed, "original-id"))
			if err != nil {
				t.Fatal(err)
			}
			id := string(raw)
			s := NewStore(managed, root, opened(t, root))
			if !slices.Contains(s.Pending(), id) {
				t.Fatal("restart lost original ID", id)
			}
			switch mode {
			case "prepared":
				if _, err := s.Discard(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			case "running":
				if _, err := s.Apply(t.Context(), id); !errors.Is(err, ErrCandidateUnsettled) {
					t.Fatalf("process exit was treated as scope completion: %v", err)
				}
				if _, err := s.Discard(t.Context(), id); !errors.Is(err, ErrCandidateUnsettled) {
					t.Fatal(err)
				}
			case "ready":
				e, err := s.Apply(t.Context(), id)
				if err != nil || e.ID != id {
					t.Fatalf("recover: %v %v", e, err)
				}
				if got := read(t, root, "a.txt"); got != "process-owned result\n" {
					t.Fatal(got)
				}
			case "cleanup":
				r, _, err := s.managedRecord(id)
				if err != nil {
					t.Fatal(err)
				}
				if out, err := s.repo.Command(t.Context(), "worktree", "unlock", r.Entry.Cand.WorktreeRoot).CombinedOutput(); err != nil {
					t.Fatalf("unlock: %v %s", err, out)
				}
				write(t, root, "a.txt", "later user edit\n")
				if _, err := s.Apply(t.Context(), id); err != nil {
					t.Fatal(err)
				}
				if got := read(t, root, "a.txt"); got != "later user edit\n" {
					t.Fatal("restart cleanup republished", got)
				}
			}
		})
	}
}
