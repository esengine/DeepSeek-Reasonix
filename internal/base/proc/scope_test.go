package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestOwnedScopeWaitOutlivesMonitorAfterCancellation(t *testing.T) {
	if os.Getenv("REASONIX_SCOPE_HELPER") == "1" {
		return
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedScopeWaitOutlivesMonitorAfterCancellation$")
	command.Env = append(os.Environ(), "REASONIX_SCOPE_HELPER=1")
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	type result struct {
		tracked *TrackedCommand
		err     error
	}
	returned := make(chan result, 1)
	go func() {
		tracked, err := RunCommand(ctx, command, RunOptions{Track: true, CancelWaitGrace: time.Millisecond,
			CancelRetryFor: time.Millisecond, CancelRetryInterval: time.Millisecond,
			CancelScope: func() error { return nil }, WaitScope: func() error { close(entered); <-release; return nil }})
		returned <- result{tracked, err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("monitor did not exit")
	}
	cancel()
	select {
	case r := <-returned:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatal(r.err)
		}
		select {
		case <-r.tracked.WaitCompleted():
			t.Fatal("monitor wait released ownership while namespace scope was live")
		default:
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancellation did not return bounded result")
	}
}

func TestUnknownScopeDoesNotPublishPhysicalCompletion(t *testing.T) {
	done, results := make(chan struct{}), make(chan error, 1)
	publishCompletedWait(done, results, ErrScopeCompletionUnknown)
	if err := <-results; !errors.Is(err, ErrScopeCompletionUnknown) {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("scope observation failure impersonated physical completion")
	default:
	}
}
