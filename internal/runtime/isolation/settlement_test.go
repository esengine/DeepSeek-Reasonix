package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func settledFixture(t *testing.T) (*Store, *Entry) {
	t.Helper()
	repo := gitRepo(t)
	c, err := mutation.New(repo, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(c)
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	return s, e
}

func TestAppliedResultRemainsIdempotentAfterCleanupAndRestart(t *testing.T) {
	s, e := settledFixture(t)
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate remains: %v", err)
	}
	write(t, s.workspace, "a.txt", "later user edit\n")
	s.Abort(t.Context(), e)
	restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	for range 2 {
		result, err := restarted.Apply(t.Context(), e.ID)
		if err != nil || result.State != Integrated {
			t.Fatalf("lost completed identity: %+v / %v", result, err)
		}
	}
	if got := read(t, s.workspace, "a.txt"); got != "later user edit\n" {
		t.Fatalf("reapplied old result: %s", got)
	}
	if ids := restarted.Pending(); len(ids) != 0 {
		t.Fatalf("settled result offered as pending: %v", ids)
	}
}

func TestSettlementProcessCrashHelper(t *testing.T) {
	repo := os.Getenv("REASONIX_SETTLEMENT_CRASH_REPO")
	if repo == "" {
		return
	}
	c, err := mutation.New(repo, os.Getenv("REASONIX_SETTLEMENT_CRASH_COORDINATOR"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(os.Getenv("REASONIX_SETTLEMENT_CRASH_MANAGED"), repo, opened(t, repo)).WithMutationCoordinator(c)
	s.cleanupProbe = func(*Entry) error { os.Exit(77); return nil }
	if os.Getenv("REASONIX_SETTLEMENT_CRASH_ACTION") == "apply" {
		_, err = s.Apply(t.Context(), os.Getenv("REASONIX_SETTLEMENT_CRASH_ID"))
	} else {
		_, err = discardReviewed(t, s, os.Getenv("REASONIX_SETTLEMENT_CRASH_ID"))
	}
	t.Fatalf("crash checkpoint not reached: %v", err)
}

func TestSettlementRecoversProcessDeathAfterCandidateDeletion(t *testing.T) {
	for _, action := range []string{"apply", "discard"} {
		t.Run(action, func(t *testing.T) {
			s, e := settledFixture(t)
			raw, err := os.ReadFile(entryPath(e.Cand))
			if err != nil {
				t.Fatal(err)
			}
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			fields["future_receipt"] = json.RawMessage(`{"keep":true}`)
			raw, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(entryPath(e.Cand), raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSettlementProcessCrashHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_SETTLEMENT_CRASH_REPO="+s.workspace,
				"REASONIX_SETTLEMENT_CRASH_MANAGED="+s.managedRoot, "REASONIX_SETTLEMENT_CRASH_COORDINATOR="+t.TempDir(),
				"REASONIX_SETTLEMENT_CRASH_ID="+e.ID, "REASONIX_SETTLEMENT_CRASH_ACTION="+action)
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 77 {
				t.Fatalf("child: %v\n%s", err, output)
			}
			if _, err := os.Stat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("did not crash after deletion: %v", err)
			}
			restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
			before, err := restarted.operationSnapshot(t.Context(), e.ID)
			if err != nil || before.CleanupComplete {
				t.Fatalf("unexpected settlement: %+v %v", before, err)
			}
			if action == "apply" && read(t, s.workspace, "a.txt") != "candidate\n" {
				t.Fatal("apply was not committed before cleanup")
			}
			write(t, s.workspace, "a.txt", "new user work\n")
			var result *Entry
			if action == "apply" {
				result, err = restarted.Apply(t.Context(), e.ID)
			} else {
				result, err = discardReviewed(t, restarted, e.ID)
			}
			if err != nil || !result.CleanupComplete {
				t.Fatalf("cleanup recovery: %+v %v", result, err)
			}
			if read(t, s.workspace, "a.txt") != "new user work\n" {
				t.Fatal("recovery rewrote source")
			}
			raw, err = os.ReadFile(entryPath(e.Cand))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["future_receipt"]) != `{"keep":true}` {
				t.Fatal("cleanup dropped unknown receipt fields")
			}
			restarted.SweepExpired(t.Context(), -time.Second)
			if _, err := restarted.operationSnapshot(t.Context(), e.ID); err != nil {
				t.Fatal("sweeper deleted terminal receipt", err)
			}
		})
	}
}

func TestAllocatedIdentityCannotBeReusedAcrossWorkspaces(t *testing.T) {
	s, e := settledFixture(t)
	if _, err := discardReviewed(t, s, e.ID); err != nil {
		t.Fatal(err)
	}
	other := NewStore(s.managedRoot, t.TempDir(), s.repo)
	unlock, err := other.lockEntry(t.Context(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := other.reserveIdentity(e.ID); err == nil {
		t.Fatal("reused an existing workspace identity")
	}
	if err := os.Remove(entryPath(e.Cand)); err != nil {
		t.Fatal(err)
	}
	if err := other.reserveIdentity(e.ID); !errors.Is(err, os.ErrExist) {
		t.Fatalf("allocated identity reused after record loss: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.managedRoot, "."+e.ID+".allocated")); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCleanupStateRetainsRecordAndCandidate(t *testing.T) {
	s, e := settledFixture(t)
	raw, err := os.ReadFile(entryPath(e.Cand))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["cleanup_complete"] = json.RawMessage(`true`)
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath(e.Cand), raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = s.Apply(t.Context(), e.ID)
	var refused tool.Refusal
	if !errors.As(err, &refused) || refused.Code != CodeUnsupportedState {
		t.Fatalf("inconsistent lifecycle accepted: %v", err)
	}
	s.SweepExpired(t.Context(), -time.Second)
	after, err := os.ReadFile(entryPath(e.Cand))
	if err != nil || string(after) != string(raw) {
		t.Fatal("invalid record overwritten", err)
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Fatal("candidate changed")
	}
}

func TestDiscardRemainsIdempotentAndCannotBeApplied(t *testing.T) {
	s, e := settledFixture(t)
	if _, err := discardReviewed(t, s, e.ID); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	if _, err := discardReviewed(t, restarted, e.ID); err != nil {
		t.Fatal(err)
	}
	_, err := restarted.Apply(t.Context(), e.ID)
	var refused tool.Refusal
	if !errors.As(err, &refused) || refused.Code != "isolation.already_settled" {
		t.Fatalf("discarded identity not retained: %v", err)
	}
	if got := read(t, s.workspace, "a.txt"); got == "candidate\n" {
		t.Fatal("discarded result was applied")
	}
}

func TestDiscardDriftRetainsOriginalSealedManifest(t *testing.T) {
	s, e := settledFixture(t)
	sealedTree := e.Tree
	write(t, e.Cand.WorkspaceRoot, "a.txt", "external candidate change\n")
	write(t, e.Cand.WorkspaceRoot, "new.txt", "external added file\n")
	result, err := discardReviewed(t, s, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tree != sealedTree || len(result.Changes) != len(e.Changes) || result.State != Discarded {
		t.Fatalf("rewrote sealed history: %+v", result)
	}
	if _, err := os.Stat(e.Cand.WorktreeRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("explicit discard retained candidate: %v", err)
	}
}

func TestPendingUsesDurableSettlementFromAnotherStore(t *testing.T) {
	s, e := settledFixture(t)
	if _, err := s.lookup(e.ID); err != nil {
		t.Fatal(err)
	}
	other := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
	if _, err := other.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if ids := s.Pending(); len(ids) != 0 {
		t.Fatalf("local cache resurrected settled result: %v", ids)
	}
}

func TestLegacySettlementMigratesReceiptAndRetainsIdentity(t *testing.T) {
	for _, action := range []string{"apply", "discard"} {
		t.Run(action, func(t *testing.T) {
			s, e := settledFixture(t)
			raw, err := os.ReadFile(entryPath(e.Cand))
			if err != nil {
				t.Fatal(err)
			}
			fields := map[string]json.RawMessage{}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "version")
			delete(fields, "state")
			delete(fields, "run_generation")
			fields["future_receipt"] = json.RawMessage(`{"keep":true}`)
			raw, err = json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			legacy := filepath.Join(filepath.Dir(e.Cand.WorktreeRoot), legacyEntryFile)
			if err := os.WriteFile(legacy, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(entryPath(e.Cand)); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(s.managedRoot, "."+e.ID+".allocated")); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				restarted := NewStore(s.managedRoot, s.workspace, s.repo).WithMutationCoordinator(s.mutations)
				if action == "apply" {
					_, err = restarted.Apply(t.Context(), e.ID)
				} else {
					_, err = discardReviewed(t, restarted, e.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("legacy marker still owns candidate: %v", err)
			}
			raw, err = os.ReadFile(entryPath(e.Cand))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["future_receipt"]) != `{"keep":true}` {
				t.Fatal("legacy migration dropped unknown data")
			}
			if _, err := os.Stat(filepath.Join(s.managedRoot, "."+e.ID+".allocated")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
