package main

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/worktree"
)

func startFinalizeAdmissionTest(t *testing.T, app *App, request worktree.CleanupRequest) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	app.ctx = ctx
	result := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := app.FinalizeWorktreeMerge(request)
		result <- err
	}()
	// Run before the test restores shared hooks, including assertion failures.
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("finalize worker did not stop after cancellation")
		}
	})
	return result
}

func waitForFinalizeAdmissionGate(t *testing.T, entered <-chan struct{}, result <-chan error) {
	t.Helper()
	select {
	case <-entered:
	case err := <-result:
		t.Fatalf("finalize returned before admission gate: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("finalize did not reach admission gate")
	}
}

func waitForFinalizeAdmissionResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("finalize did not complete after gate release")
		return nil
	}
}
