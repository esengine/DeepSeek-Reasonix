package isolation

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBeginPersistsIdentityBeforeRunning(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), e) })
	other := NewStore(managed, repo, opened(t, repo))
	if ids := other.Pending(); len(ids) != 1 || ids[0] != e.ID {
		t.Fatalf("running candidate has no durable identity: %v", ids)
	}
}

func TestRunnerPanicRetainsResultAndReleasesLiveness(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, repo, opened(t, repo))
	_, err := s.Execute(t.Context(), func(_ context.Context, run Run) (Outcome, error) {
		write(t, run.WorkspaceRoot, "a.txt", "valuable work\n")
		panic("runner failure")
	}, repo, "work", "")
	if err == nil {
		t.Fatal("panic was reported as success")
	}
	ids := s.Pending()
	if len(ids) != 1 || !strings.Contains(err.Error(), ids[0]) {
		t.Fatalf("recovery identity lost: %v %v", ids, err)
	}
	reopened := NewStore(managed, repo, opened(t, repo))
	entry, err := reopened.lookup(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, entry.Cand.WorkspaceRoot, "a.txt"); got != "valuable work\n" {
		t.Fatal(got)
	}
	if _, err := reopened.Discard(t.Context(), ids[0]); err != nil {
		t.Fatalf("panic retained liveness lock: %v", err)
	}
}

func TestFinishFailurePreservesChangedWorktree(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "valuable work\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Finish(ctx, e); err == nil {
		t.Fatal("cancelled finish succeeded")
	}
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "valuable work\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(entryPath(e.Cand)); err != nil {
		t.Fatalf("failed finish forgot result identity: %v", err)
	}
}

func TestExpiredUnintegratedChangesAreRetained(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.Begin(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "valuable work\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(entryPath(e.Cand), past, past); err != nil {
		t.Fatal(err)
	}
	s.SweepExpired(t.Context(), 14*24*time.Hour)
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "valuable work\n" {
		t.Fatal(got)
	}
}
