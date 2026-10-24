package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func TestBaselineSnapshotExcludesCoordinatedWriterThroughoutRead(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	coordinator, err := mutation.New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	queued := make(chan struct{}, 1)
	writer, err := mutation.New(root, state, func(n mutation.Notice) {
		if n.State == "waiting" {
			queued <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{WriteWorkspaceRoot: root, MutationCoordinator: coordinator}, event.Discard)
	entered, resume := make(chan struct{}), make(chan struct{})
	readDone := make(chan evidence.ContentSnapshot, 1)
	go func() {
		readDone <- a.withBaselineSnapshot(t.Context(), root, func(ctx context.Context) evidence.ContentSnapshot {
			if !mutation.HoldingLease(ctx) {
				return evidence.ContentSnapshot{Reason: "uncoordinated"}
			}
			close(entered)
			<-resume
			return evidence.CaptureContentSnapshot(ctx, root, 50_000)
		})
	}()
	<-entered
	written := make(chan error, 1)
	go func() {
		lease, err := writer.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path}, CallID: "competing.writer"})
		if err == nil {
			err = os.WriteFile(path, []byte("after"), 0600)
			lease.End(mutation.Committed)
		}
		written <- err
	}()
	select {
	case <-queued:
	case <-t.Context().Done():
		t.Fatal("writer never queued")
	}
	before, err := os.ReadFile(path)
	if err != nil || string(before) != "before" {
		t.Fatalf("writer entered snapshot: %q %v", before, err)
	}
	close(resume)
	if got := <-readDone; !got.Complete {
		t.Fatalf("snapshot failed: %+v", got)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestBaselineSnapshotDoesNotExpandOrWaitOnAnExistingPartialLease(t *testing.T) {
	root := t.TempDir()
	coordinator, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	held, err := coordinator.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(root, "one")}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{WriteWorkspaceRoot: root, MutationCoordinator: coordinator}, event.Discard)
	called := false
	got := a.withBaselineSnapshot(held.Context(t.Context()), root, func(context.Context) evidence.ContentSnapshot {
		called = true
		return evidence.ContentSnapshot{Complete: true}
	})
	if called || got.Complete {
		t.Fatalf("partial lease expanded or bypassed: %+v", got)
	}
}
