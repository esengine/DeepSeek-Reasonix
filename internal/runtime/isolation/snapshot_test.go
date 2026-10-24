package isolation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/state/mutation"
)

func TestCandidateSnapshotWaitsForParticipatingWriter(t *testing.T) {
	repo := gitRepo(t)
	queued := make(chan struct{}, 1)
	coordinator, err := mutation.New(repo, t.TempDir(), func(n mutation.Notice) {
		if n.State == "waiting" {
			queued <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(repo, "a.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.End(mutation.Committed)
	store := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(coordinator)
	type result struct {
		entry *Entry
		err   error
	}
	done := make(chan result, 1)
	go func() { entry, err := store.Begin(t.Context(), repo); done <- result{entry, err} }()
	select {
	case <-queued:
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot bypassed the active writer")
	}
	write(t, repo, "a.txt", "completed write\n")
	lease.End(mutation.Committed)
	started := <-done
	if started.err != nil {
		t.Fatal(started.err)
	}
	t.Cleanup(func() { store.Abort(t.Context(), started.entry) })
	if got := read(t, started.entry.Cand.WorkspaceRoot, "a.txt"); got != "completed write\n" {
		t.Fatalf("snapshot content: %q", got)
	}
	next, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(repo, "a.txt")}})
	if err != nil {
		t.Fatalf("candidate retained source lease: %v", err)
	}
	next.End(mutation.NotExecuted)
}

func TestFinishWaitsForCancelledWriterCleanupBeforeSealingCandidate(t *testing.T) {
	repo, state := gitRepo(t), t.TempDir()
	waiting := make(chan struct{}, 1)
	c, err := mutation.New(repo, state, func(n mutation.Notice) {
		if n.CallID == "isolation.finish" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(c)
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	writer, err := mutation.New(e.Cand.WorkspaceRoot, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := writer.Acquire(t.Context(), mutation.Intent{Effect: mutation.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	settled := make(chan struct{})
	if err := l.HoldUntil(settled); err != nil {
		t.Fatal(err)
	}
	l.End(mutation.Unknown)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := s.Finish(ctx, e); finished <- err }()
	select {
	case <-waiting:
	case err := <-finished:
		t.Fatalf("sealed before cleanup: %v", err)
	case <-ctx.Done():
		t.Fatal("missing finish wait")
	}
	source, err := c.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(repo, "a.txt")}})
	if err != nil {
		t.Fatal("candidate settlement blocked source code writes", err)
	}
	source.End(mutation.NotExecuted)
	write(t, e.Cand.WorkspaceRoot, "late.txt", "last write\n")
	close(settled)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range e.Changes {
		if change.Path == "late.txt" {
			found = true
		}
	}
	if !found {
		t.Fatal("finished manifest missed last write")
	}
}
