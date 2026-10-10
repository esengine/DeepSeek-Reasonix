package workspacelease

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// With the lease off, no cross-session lease is taken at all; the default
// still queues behind the holder.
func TestWithoutWriteSerializationSkipsTheLease(t *testing.T) {
	lockDir := t.TempDir()
	root := t.TempDir()

	// The lease is held only while a run is active: with one, AcquireWrite
	// keeps the lease until the final run ends.
	holder, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	holder.BeginRun()
	defer holder.EndRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	off, err := New(root, lockDir, nil, WithoutWriteSerialization())
	if err != nil {
		t.Fatalf("off owner: %v", err)
	}
	if err := off.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("with the lease off the write is granted at once: %v", err)
	}
	if st := off.State(); st.Acquired || st.Waiting {
		t.Fatalf("nothing should be held or waited on: %+v", st)
	}

	on, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("on owner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := on.AcquireWrite(ctx); err == nil {
		t.Fatal("the default must still wait behind the holder")
	}
}

// Optimistic drops only the whole-workspace hold: an undeclaring writer runs
// beside a session that holds the lease, while a writer that declares paths
// still waits behind it.
func TestWithoutWholeWorkspaceHoldRelaxesOnlyTheWholeClaim(t *testing.T) {
	lockDir := t.TempDir()
	root := t.TempDir()

	holder, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	holder.BeginRun()
	defer holder.EndRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	optimistic, err := New(root, lockDir, nil, WithoutWholeWorkspaceHold())
	if err != nil {
		t.Fatalf("optimistic owner: %v", err)
	}
	if err := optimistic.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("an undeclaring writer must not queue behind the holder: %v", err)
	}
	if st := optimistic.State(); st.Acquired || st.Waiting {
		t.Fatalf("nothing should be held or waited on: %+v", st)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := optimistic.AcquirePaths(ctx, []string{filepath.Join(root, "a.txt")}); err == nil {
		t.Fatal("a writer that declares a path must still wait behind the whole-workspace holder")
	}
}

// A declared extent the host cannot resolve — a path outside the workspace,
// for example — falls back to the whole-workspace hold instead of silently
// taking no lock at all.
func TestUnresolvableDeclaredPathsStillHoldUnderOptimistic(t *testing.T) {
	lockDir := t.TempDir()
	root := t.TempDir()

	holder, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	holder.BeginRun()
	defer holder.EndRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	optimistic, err := New(root, lockDir, nil, WithoutWholeWorkspaceHold())
	if err != nil {
		t.Fatalf("optimistic owner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := optimistic.AcquirePaths(ctx, []string{filepath.Join(t.TempDir(), "elsewhere.txt")}); err == nil {
		t.Fatal("an unresolvable declared path must still wait behind the holder")
	}
}
