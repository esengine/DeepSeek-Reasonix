package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func TestPreparingRecordPrecedesGitMutationAndPinsIdentity(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	s.preparationProbe = func(stage State, e *Entry) {
		if stage != Preparing {
			return
		}
		if _, err := os.Stat(e.Cand.WorktreeRoot); !os.IsNotExist(err) {
			t.Fatalf("Git ran before preparing record: %v", err)
		}
		raw, err := os.ReadFile(entryRecordPath(e))
		if err != nil {
			t.Fatal(err)
		}
		var d durableEntry
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatal(err)
		}
		if d.State != Preparing || d.RunGeneration == "" || d.Cand.Repo.Valid() {
			t.Fatalf("preparing identity: %+v", d)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		cand := fields["cand"].(map[string]any)
		cand["future"] = "candidate"
		cand["Repo"].(map[string]any)["future"] = "identity"
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entryRecordPath(e), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	if e.State != Running || !e.Cand.Repo.Valid() {
		t.Fatalf("prepared: %+v", e)
	}
	raw, err := os.ReadFile(entryRecordPath(e))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	cand := fields["cand"].(map[string]any)
	if cand["future"] != "candidate" || cand["Repo"].(map[string]any)["future"] != "identity" {
		t.Fatal("pinning identity lost unknown fields")
	}
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
}

func TestPreparationCancellationRetainsRecoverableRecord(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.preparationProbe = func(stage State, _ *Entry) {
		if stage == Preparing {
			cancel()
		}
	}
	if _, err := s.BeginOwned(ctx, repo, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatalf("preparation: %v", err)
	}
	ids := s.Pending()
	if len(ids) != 1 {
		t.Fatalf("lost interrupted preparation: %v", ids)
	}
	e, err := s.lookup(ids[0])
	if err != nil || e.State != RecoveryRequired {
		t.Fatalf("recovery: %+v %v", e, err)
	}
	if _, err := s.Discard(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Pending()) != 0 {
		t.Fatal("discard retained preparation")
	}
}

func TestPreparationCancelledAfterSnapshotRemovesEmptyAllocation(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	coordinator, err := mutation.New(repo, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "isolation.snapshot" && n.State == "released" {
			cancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(managed, repo, opened(t, repo)).WithMutationCoordinator(coordinator)
	if _, err := s.BeginOwned(ctx, repo, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation: %v", err)
	}
	entries, err := os.ReadDir(managed)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unrecorded empty allocation remains: %s", entry.Name())
		}
	}
}

func TestPreparingProcessCrashHelper(t *testing.T) {
	repo := os.Getenv("REASONIX_PREPARING_CRASH_REPO")
	if repo == "" {
		return
	}
	s := NewStore(os.Getenv("REASONIX_PREPARING_CRASH_MANAGED"), repo, opened(t, repo))
	s.preparationProbe = func(stage State, _ *Entry) {
		if string(stage) == os.Getenv("REASONIX_PREPARING_CRASH_STAGE") {
			os.Exit(77)
		}
	}
	if _, err := s.BeginOwned(t.Context(), repo, "owner"); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash checkpoint was not reached")
}

func TestPreparingProcessDeathRetainsAndDiscardsEnvironment(t *testing.T) {
	for _, stage := range []State{Preparing, Running} {
		t.Run(string(stage), func(t *testing.T) {
			repo, managed := gitRepo(t), t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreparingProcessCrashHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_PREPARING_CRASH_REPO="+repo, "REASONIX_PREPARING_CRASH_MANAGED="+managed, "REASONIX_PREPARING_CRASH_STAGE="+string(stage))
			out, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 77 {
				t.Fatalf("child exit: %v\n%s", err, out)
			}
			s := NewStore(managed, repo, opened(t, repo))
			ids := s.Pending()
			if len(ids) != 1 {
				t.Fatalf("orphaned preparation: %v", ids)
			}
			s.SweepExpired(t.Context(), -time.Second)
			e, err := s.lookup(ids[0])
			if err != nil || e.State != RecoveryRequired {
				t.Fatalf("recovered: %+v %v", e, err)
			}
			_, err = s.Resume(t.Context(), e.ID, "owner")
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != CodeRecoveryRequired {
				t.Fatalf("unfinished preparation resumed: %v", err)
			}
			if _, err := s.Apply(t.Context(), e.ID); err == nil {
				t.Fatal("unfinished preparation applied")
			}
			if stage == Running {
				if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "one\n" {
					t.Fatal(got)
				}
			}
			if _, err := s.Discard(t.Context(), e.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(e.Cand.WorktreeRoot); !os.IsNotExist(err) {
				t.Fatalf("discard left candidate files: %v", err)
			}
			if receipt, err := s.operationSnapshot(t.Context(), e.ID); err != nil || !receipt.CleanupComplete {
				t.Fatalf("lost discard receipt: %+v %v", receipt, err)
			}
			if len(s.Pending()) != 0 {
				t.Fatal("discard left lifecycle record")
			}
		})
	}
}
