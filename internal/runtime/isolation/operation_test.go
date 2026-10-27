package isolation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func finishedOperationResult(t *testing.T) (*Store, *Entry) {
	t.Helper()
	repo, managed := gitRepo(t), testenv.TempDir(t)
	s := NewStore(managed, repo, opened(t, repo))
	report, err := s.Execute(t.Context(), writes(map[string]string{"a.txt": "candidate\n"}), repo, "change a", "")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.lookup(idOf(t, report))
	if err != nil {
		t.Fatal(err)
	}
	return s, e
}

func requireResultApplyWaitsAndCancels(t *testing.T, s *Store, id string) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := s.Apply(ctx, id)
			done <- err
		}()
		synctest.Wait()
		select {
		case err := <-done:
			t.Errorf("Apply passed an active operation owner: %v", err)
			return
		default:
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("queued Apply cancellation = %v", err)
		}
	})
}

func TestApplyWaitsForTheResultOwnerBeforeMutation(t *testing.T) {
	s, e := finishedOperationResult(t)
	release, err := s.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	peer := NewStore(s.managedRoot, s.workspace, s.repo)
	requireResultApplyWaitsAndCancels(t, peer, e.ID)
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Errorf("unadmitted Apply changed source: %q", got)
	}
	if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
		t.Fatalf("unadmitted Apply deleted candidate: %v", err)
	}
	release()
	if _, err := peer.Apply(t.Context(), e.ID); err != nil {
		t.Fatalf("admitted Apply did not resume normally: %v", err)
	}
}

func TestResultOperationAdmissionPreservesTheActiveOwner(t *testing.T) {
	for _, operation := range []string{"apply", "discard", "paths", "empty_paths", "abort", "sweep"} {
		t.Run(operation, func(t *testing.T) {
			owner, e := finishedOperationResult(t)
			if operation == "empty_paths" {
				write(t, e.Cand.WorkspaceRoot, "a.txt", "one\n")
			}
			release, err := owner.acquireResultOperation(t.Context(), e.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			other := NewStore(filepath.Join(owner.managedRoot, "."), owner.workspace, owner.repo)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			switch operation {
			case "apply":
				_, err = other.Apply(ctx, e.ID)
			case "discard":
				_, err = other.Discard(ctx, e.ID)
			case "paths", "empty_paths":
				_, err = NewApplyTool(other).DeclaredWritePaths(ctx, args(e.ID))
			case "abort":
				other.Abort(ctx, e)
			case "sweep":
				other.SweepExpired(t.Context(), -time.Second)
			}
			if operation != "abort" && operation != "sweep" && !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled admission returned %v", err)
			}
			if got := read(t, owner.workspace, "a.txt"); got != "one\n" {
				t.Errorf("blocked result wrote the source: %q", got)
			}
			if _, err := os.Stat(entryPath(e.Cand)); err != nil {
				t.Errorf("blocked result lost its publication: %v", err)
			}
			if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
				t.Errorf("blocked result lost its candidate: %v", err)
			}
			if !slices.Contains(other.Pending(), e.ID) {
				t.Error("blocked result disappeared from durable pending")
			}
			if next, err := other.acquireResultOperation(t.Context(), e.ID, true); !errors.Is(err, filelock.ErrHeld) {
				if next != nil {
					next()
				}
				t.Errorf("cancelled or skipped operation released the active owner: %v", err)
			}
		})
	}
}

func TestResultOperationsOnOtherIDsProgressAndCleanupRemainsActive(t *testing.T) {
	owner, e := finishedOperationResult(t)
	report, err := owner.Execute(t.Context(), writes(map[string]string{"b.txt": "other candidate\n"}), owner.workspace, "change b", "")
	if err != nil {
		t.Fatal(err)
	}
	otherID := idOf(t, report)
	otherEntry, err := owner.lookup(otherID)
	if err != nil {
		t.Fatal(err)
	}
	release, err := owner.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	peer := NewStore(owner.managedRoot, owner.workspace, owner.repo)
	if _, err := peer.Apply(t.Context(), otherID); err != nil {
		t.Fatalf("unrelated ID could not progress: %v", err)
	}
	if got := read(t, owner.workspace, "b.txt"); got != "other candidate\n" {
		t.Fatalf("unrelated ID did not apply: %q", got)
	}
	if _, err := os.Stat(otherEntry.Cand.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("unrelated settlement retained its removed candidate: %v", err)
	}
	peer.SweepExpired(t.Context(), -time.Second)
	if _, err := os.Stat(e.Cand.WorktreeRoot); err != nil {
		t.Fatalf("sweep deleted an active operation's candidate: %v", err)
	}
	release()
	peer.SweepExpired(t.Context(), -time.Second)
	if _, err := os.Stat(e.Cand.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("sweep no longer removes an unlocked expired legacy result: %v", err)
	}
}

func TestUnadmittedFirstFinishRemainsAnExplicitLiveRetry(t *testing.T) {
	repo, managed := gitRepo(t), testenv.TempDir(t)
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	release, err := s.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	peer := NewStore(managed, repo, opened(t, repo))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	kept, finishErr := peer.Finish(ctx, e)
	if !kept || !errors.Is(finishErr, context.Canceled) {
		t.Errorf("unadmitted Finish = %v, %v", kept, finishErr)
	}
	for _, path := range []string{entryPath(e.Cand), inspectionPath(e.Cand)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("unadmitted Finish published without the operation lease: %v", err)
		}
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Errorf("unadmitted Finish lost bytes: %q", got)
	}
	if !slices.Contains(peer.Pending(), e.ID) || slices.Contains(NewStore(managed, repo, opened(t, repo)).Pending(), e.ID) {
		t.Error("unadmitted Finish misrepresented live-only recovery as durable")
	}
	release()
	if kept, err := peer.Finish(t.Context(), e); !kept || err != nil {
		t.Fatalf("same-ID Finish retry failed: %v %v", kept, err)
	}
	if !slices.Contains(NewStore(managed, repo, opened(t, repo)).Pending(), e.ID) {
		t.Fatal("admitted Finish did not publish the same ID durably")
	}
}

func TestResultOperationRechecksCancellationAfterFreeAdmission(t *testing.T) {
	s, e := finishedOperationResult(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Apply(ctx, e.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("already-cancelled free admission = %v", err)
	}
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Fatalf("already-cancelled operation wrote source: %q", got)
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatalf("cancelled free admission retained the operation lease: %v", err)
	}
}

func TestResultOperationKeepsItsLockIdentityAfterSettlement(t *testing.T) {
	s, e := finishedOperationResult(t)
	path := filepath.Join(s.managedRoot, resultLockDirectory, resultLockName(e.ID))
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("settlement replaced or deleted the mutex inode: %v", err)
	}
	peer := NewStore(s.managedRoot, s.workspace, s.repo)
	successor, err := peer.acquireResultOperation(t.Context(), e.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer successor()
	release()
	if extra, err := s.acquireResultOperation(t.Context(), e.ID, true); !errors.Is(err, filelock.ErrHeld) {
		if extra != nil {
			extra()
		}
		t.Fatalf("old release freed a successor's OS lease: %v", err)
	}
}

func TestResultOperationRejectsAnInvalidLockNamespaceWithoutDeletingWork(t *testing.T) {
	s, e := finishedOperationResult(t)
	path := filepath.Join(s.managedRoot, resultLockDirectory)
	if err := os.Rename(path, path+"-backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep unknown data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), e.ID); !errors.Is(err, os.ErrInvalid) {
		t.Errorf("invalid namespace error = %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep unknown data" {
		t.Fatalf("invalid namespace rewritten: %q %v", data, err)
	}
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Error("unadmitted operation wrote source")
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "candidate\n" {
		t.Error("unadmitted operation deleted candidate")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+"-backup", path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatalf("restored original namespace did not recover: %v", err)
	}
}

func TestResultOperationSettlesTheCanonicalObservedID(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "discard"}[discard], func(t *testing.T) {
			s, e := finishedOperationResult(t)
			id := " \t" + e.ID + "\n"
			var err error
			if discard {
				_, err = s.Discard(t.Context(), id)
			} else {
				_, err = s.Apply(t.Context(), id)
			}
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(s.Pending(), e.ID) {
				t.Fatal("settlement used unnormalized input instead of the observed result ID")
			}
			if _, err := os.Stat(e.Cand.WorktreeRoot); !os.IsNotExist(err) {
				t.Fatalf("canonical result retained after settlement: %v", err)
			}
		})
	}
}

func TestUnknownResultRequestsDoNotAllocatePermanentLocks(t *testing.T) {
	s, e := finishedOperationResult(t)
	for i := range 32 {
		_, err := s.Apply(t.Context(), fmt.Sprintf("unknown-%d", i))
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != CodeUnknownID {
			t.Fatalf("unknown result error = %v", err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(s.managedRoot, resultLockDirectory))
	if err != nil || len(entries) != 1 || entries[0].Name() != resultLockName(e.ID) {
		t.Fatalf("unknown requests grew permanent lock metadata: %v %v", entries, err)
	}
}
