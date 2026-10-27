package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContinuationRetainsEmptyWorktreeAcrossRestart(t *testing.T) {
	workspace := gitRepo(t)
	managed := t.TempDir()
	s := NewStore(managed, workspace, opened(t, workspace))
	var first Run
	_, err := s.ExecuteContinuation(context.Background(), func(_ context.Context, r Run) (Outcome, error) {
		first = r
		return Outcome{Answer: "ready"}, nil
	}, workspace, "inspect", "", "parent", "")
	if err != nil {
		t.Fatal(err)
	}
	s = NewStore(managed, workspace, opened(t, workspace))
	if ids := s.Pending(); len(ids) != 1 || ids[0] != first.ID {
		t.Fatalf("pending=%v", ids)
	}
	_, err = s.ExecuteContinuation(context.Background(), func(_ context.Context, r Run) (Outcome, error) {
		if r.ID != first.ID || r.WorkspaceRoot != first.WorkspaceRoot {
			t.Fatalf("new worktree: %+v", r)
		}
		write(t, r.WorkspaceRoot, "a.txt", "continued\n")
		return Outcome{}, nil
	}, workspace, "continue", "", "parent", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, workspace, "a.txt"); got != "one\n" {
		t.Fatal(got)
	}
	if _, err := s.Apply(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, workspace, "a.txt"); got != "continued\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(managed, settlementFenceDirectory, continuationName(first.ID))); !os.IsNotExist(err) {
		t.Fatalf("fence retained: %v", err)
	}
}

func TestContinuationPreservesUnknownInspectionAndPanicWrites(t *testing.T) {
	workspace := gitRepo(t)
	s := NewStore(t.TempDir(), workspace, opened(t, workspace))
	var run Run
	_, err := s.ExecuteContinuation(context.Background(), func(_ context.Context, r Run) (Outcome, error) {
		run = r
		write(t, r.WorkspaceRoot, "a.txt", "partial")
		panic("fixture")
	}, workspace, "", "", "owner", "")
	if err == nil {
		t.Fatal("panic lost")
	}
	if got := read(t, run.WorkspaceRoot, "a.txt"); got != "partial" {
		t.Fatal(got)
	}
	record, ok := s.readContinuation(run.ID)
	if !ok {
		t.Fatal("panic lost continuation")
	}
	raw, err := json.Marshal(inspectionRecord{Version: 999, Entry: record.Entry})
	if err != nil {
		t.Fatal(err)
	}
	path := inspectionPath(record.Entry.Cand)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(s.managedRoot, workspace, opened(t, workspace))
	if _, err := peer.Discard(context.Background(), run.ID); err == nil {
		t.Fatal("unknown inspection bypassed by continuation")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(raw) {
		t.Fatal("unknown record changed")
	}
	if got := read(t, run.WorkspaceRoot, "a.txt"); got != "partial" {
		t.Fatal(got)
	}
}

func TestContinuationRejectsForeignOwnerAndProtectsRunningWork(t *testing.T) {
	workspace := gitRepo(t)
	managed := t.TempDir()
	s := NewStore(managed, workspace, opened(t, workspace))
	report, err := s.ExecuteContinuation(context.Background(), writes(map[string]string{"a.txt": "first"}), workspace, "first", "", "parent", "")
	if err != nil {
		t.Fatal(err)
	}
	id := idOf(t, report)
	called := false
	_, err = s.ExecuteContinuation(context.Background(), func(context.Context, Run) (Outcome, error) { called = true; return Outcome{}, nil }, workspace, "", "", "other", id)
	if err == nil || called {
		t.Fatalf("foreign admitted: %v", err)
	}
	entered, unblock, done := make(chan Run, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := s.ExecuteContinuation(context.Background(), func(_ context.Context, r Run) (Outcome, error) {
			entered <- r
			<-unblock
			return Outcome{}, context.Canceled
		}, workspace, "", "", "parent", id)
		done <- err
	}()
	r := <-entered
	other := NewStore(managed, workspace, opened(t, workspace))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := other.Discard(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("discard=%v", err)
	}
	other.SweepExpired(context.Background(), -time.Hour)
	if _, err := os.Stat(r.WorkspaceRoot); err != nil {
		t.Fatal(err)
	}
	close(unblock)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := other.Discard(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_, err = other.ExecuteContinuation(context.Background(), nil, workspace, "", "", "parent", id)
	if err == nil {
		t.Fatal("settled result resumed")
	}
}
