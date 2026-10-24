package proc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForTrackedCommandReturnsWhenWaitStallsAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tracked := &TrackedCommand{}
	releaseWait := make(chan struct{})
	defer close(releaseWait)

	start := time.Now()
	err := waitForTrackedCommand(ctx, tracked, func() error {
		<-releaseWait
		return nil
	}, 20*time.Millisecond, time.Millisecond, time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("cancelled stalled wait returned too slowly: %v", elapsed)
	}
	if tracked.Diagnostics().KillCalls == 0 {
		t.Fatal("tracked command was not killed")
	}
	if !tracked.Diagnostics().CancelWaitGraceExpired {
		t.Fatal("cancelled stalled wait did not record grace expiry")
	}
}

func TestCancellationGraceDoesNotSignalCompletedWait(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tracked := &TrackedCommand{}
	releaseWait := make(chan struct{})
	err := waitForTrackedCommand(ctx, tracked, func() error { <-releaseWait; return nil }, time.Millisecond, time.Millisecond, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		close(releaseWait)
		t.Fatal(err)
	}
	done := tracked.WaitCompleted()
	if done == nil {
		close(releaseWait)
		t.Fatal("missing completion ownership")
	}
	select {
	case <-done:
		close(releaseWait)
		t.Fatal("grace expiry impersonated process completion")
	default:
	}
	released := make(chan struct{})
	AfterWait(done, func() { close(released) })
	select {
	case <-released:
		close(releaseWait)
		t.Fatal("cleanup ran before wait completion")
	default:
	}
	close(releaseWait)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("completed wait did not release cleanup")
	}
}

func TestCompletionSignalPrecedesPublishingWaitResult(t *testing.T) {
	done := make(chan struct{})
	results := make(chan error) // Hold publication until the assertion completes.
	finished := make(chan struct{})
	go func() {
		publishCompletedWait(done, results, nil)
		close(finished)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		// Let the producer exit even if ordering regresses.
		<-results
		t.Fatal("completion was held behind result publication")
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	<-finished
}

func TestWaitForTrackedCommandKeepsWaitErrorAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tracked := &TrackedCommand{}
	waitStarted := make(chan struct{})
	releaseWait := make(chan struct{})
	errWait := errors.New("wait failed after cancel")
	done := make(chan error, 1)

	go func() {
		done <- waitForTrackedCommand(ctx, tracked, func() error {
			close(waitStarted)
			<-releaseWait
			return errWait
		}, time.Second, time.Millisecond, time.Millisecond)
	}()

	<-waitStarted
	cancel()
	waitUntilTrackedCommandKilled(t, tracked)
	close(releaseWait)

	select {
	case err := <-done:
		if err.Error() != context.Canceled.Error() {
			t.Fatalf("error text = %q, want %q", err.Error(), context.Canceled.Error())
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if !errors.Is(err, errWait) {
			t.Fatalf("error = %v, want wrapped wait error %v", err, errWait)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled command wait")
	}
}

func waitUntilTrackedCommandKilled(t *testing.T, tracked *TrackedCommand) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		if tracked.Diagnostics().KillCalls > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for tracked command kill")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
