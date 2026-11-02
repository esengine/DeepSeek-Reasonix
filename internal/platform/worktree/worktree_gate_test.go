package worktree

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func committedSnapshot(t *testing.T) Snapshot {
	t.Helper()
	requireGit(t)
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	writeRepoFile(t, repo, "a.txt", "one\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	snap, err := TakeSnapshot(context.Background(), opened(t, repo))
	if err != nil {
		t.Fatalf("TakeSnapshot: %v", err)
	}
	return snap
}

func createInBackground(ctx context.Context, t *testing.T, snap Snapshot) <-chan error {
	done := make(chan error, 1)
	managed := filepath.Join(testenv.TempDir(t), "candidates")
	go func() {
		_, err := CreateCandidate(ctx, snap, managed)
		done <- err
	}()
	return done
}

// A held gate stands in for another worktree command of the same repository
// still running, such as a parallel isolated task creating its candidate.
func TestCandidateCreationWaitsForItsRepositorysWorktreeCommand(t *testing.T) {
	snap := committedSnapshot(t)
	gate := worktreeGate(snap.Repo.CommonDir)
	gate <- struct{}{}
	done := createInBackground(context.Background(), t, snap)
	select {
	case err := <-done:
		t.Fatalf("a candidate was created while another worktree command of its repository ran (err %v)", err)
	case <-time.After(500 * time.Millisecond):
	}
	<-gate
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CreateCandidate: %v", err)
		}
	case <-time.After(testenv.Budget(t)):
		t.Fatal("candidate creation never resumed after the repository's worktree command finished")
	}
}

func TestAnotherRepositorysWorktreeCommandDoesNotHoldACandidateBack(t *testing.T) {
	busy := committedSnapshot(t)
	gate := worktreeGate(busy.Repo.CommonDir)
	gate <- struct{}{}
	defer func() { <-gate }()
	select {
	case err := <-createInBackground(context.Background(), t, committedSnapshot(t)):
		if err != nil {
			t.Fatalf("CreateCandidate: %v", err)
		}
	case <-time.After(testenv.Budget(t)):
		t.Fatal("a candidate waited on another repository's worktree command")
	}
}

func TestCandidateCreationWaitingItsTurnStopsWithItsContext(t *testing.T) {
	snap := committedSnapshot(t)
	gate := worktreeGate(snap.Repo.CommonDir)
	gate <- struct{}{}
	defer func() { <-gate }()
	ctx, cancel := context.WithCancel(context.Background())
	done := createInBackground(ctx, t, snap)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CreateCandidate = %v, want context.Canceled", err)
		}
	case <-time.After(testenv.Budget(t)):
		t.Fatal("a cancelled candidate creation kept waiting for its turn")
	}
}
