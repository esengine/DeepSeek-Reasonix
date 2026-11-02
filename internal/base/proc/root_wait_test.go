package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func assertRootWaitClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()
	if done == nil {
		t.Fatal("registered root wait has no completion signal")
	}
	select {
	case <-done:
	default:
		t.Fatal("root wait completed without closing its signal")
	}
}

func TestRootWaitCompletionPrecedesResultAndRetainsWaitError(t *testing.T) {
	for _, errWait := range []error{nil, errors.New("root wait failed")} {
		tracked := &TrackedCommand{}
		err := waitForTrackedCommand(t.Context(), tracked, func() error { return errWait }, time.Second, time.Second, time.Second)
		if !errors.Is(err, errWait) {
			t.Fatalf("wait error changed: %v", err)
		}
		done := tracked.RootWaitCompleted()
		assertRootWaitClosed(t, done)
		if tracked.RootWaitCompleted() != done {
			t.Fatal("completion identity changed")
		}
	}
}

func TestRootWaitPublicationDoesNotDependOnReceiver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		done, results := make(chan struct{}), make(chan error)
		expected := errors.New("root wait result")
		go publishRootWait(done, results, expected)
		defer func() {
			if err := <-results; !errors.Is(err, expected) {
				t.Fatalf("result changed: %v", err)
			}
		}()
		synctest.Wait()
		assertRootWaitClosed(t, done)
	})
}

func TestRootWaitCompletionSurvivesCancellationAndRetryExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		tracked := &TrackedCommand{}
		started, unblock := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(unblock) })
		defer release()
		returned := make(chan error, 1)
		go func() {
			returned <- waitForTrackedCommand(ctx, tracked, func() error {
				close(started)
				<-unblock
				return nil
			}, time.Second, time.Second, 3*time.Second)
		}()
		<-started
		done := tracked.RootWaitCompleted()
		if done == nil {
			t.Fatal("wait signal was not published before entering Wait")
		}
		cancel()
		if err := <-returned; !errors.Is(err, context.Canceled) {
			t.Fatalf("logical cancellation result changed: %v", err)
		}
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("bounded cancellation was mistaken for root completion")
		default:
		}
		time.Sleep(4 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("retry expiry was mistaken for root completion")
		default:
		}
		release()
		synctest.Wait()
		assertRootWaitClosed(t, done)
		if done != tracked.RootWaitCompleted() {
			t.Fatal("late completion replaced the registered signal")
		}
	})
}

func TestRootWaitCompletionDoesNotChangeCancelledWaitErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		tracked := &TrackedCommand{}
		entered, release := make(chan struct{}), make(chan struct{})
		errWait := errors.New("root exited after cancellation")
		returned := make(chan error, 1)
		go func() {
			returned <- waitForTrackedCommand(ctx, tracked, func() error {
				close(entered)
				<-release
				return errWait
			}, time.Hour, time.Hour, time.Hour)
		}()
		<-entered
		cancel()
		synctest.Wait()
		close(release)
		if err := <-returned; !errors.Is(err, context.Canceled) || !errors.Is(err, errWait) {
			t.Fatalf("cancel/wait identities changed: %v", err)
		}
		assertRootWaitClosed(t, tracked.RootWaitCompleted())
	})
}

func TestRootWaitUnregisteredIsNotCompleted(t *testing.T) {
	for _, tracked := range []*TrackedCommand{nil, {}} {
		if done := tracked.RootWaitCompleted(); done != nil {
			t.Fatal("unregistered wait fabricated completion")
		}
	}
}

func TestRootWaitCommandHelper(t *testing.T) {
	if mode := os.Getenv("REASONIX_ROOT_WAIT_HELPER"); mode != "" {
		if mode == "error" {
			os.Exit(7)
		}
		os.Exit(0)
	}
}

func TestRootWaitCompletionOnRealCommands(t *testing.T) {
	for _, mode := range []string{"success", "error"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRootWaitCommandHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_ROOT_WAIT_HELPER="+mode)
			tracked, err := RunCommand(t.Context(), cmd, RunOptions{Track: true})
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode == "error" {
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != 7 {
					t.Fatalf("exit error changed: %v", err)
				}
			}
			if cmd.ProcessState == nil {
				t.Fatal("real command was not reaped")
			}
			assertRootWaitClosed(t, tracked.RootWaitCompleted())
		})
	}
}

func TestRootWaitDoesNotRegisterBeforeStartOrForUntrackedCommands(t *testing.T) {
	for _, scenario := range []string{"cancelled_before_start", "missing_executable", "untracked"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRootWaitCommandHelper$")
			cmd.Env = append(os.Environ(), "REASONIX_ROOT_WAIT_HELPER=success")
			if scenario == "cancelled_before_start" {
				cancel()
			}
			if scenario == "missing_executable" {
				cmd.Path = t.TempDir() + string(os.PathSeparator) + "missing"
			}
			tracked, err := RunCommand(ctx, cmd, RunOptions{Track: scenario != "untracked"})
			if scenario != "untracked" && err == nil {
				t.Fatal("unexpected successful launch")
			}
			if scenario == "untracked" && (tracked != nil || err != nil) {
				t.Fatalf("untracked contract changed: %v", err)
			}
			if tracked.RootWaitCompleted() != nil {
				t.Fatal("unregistered wait has completion proof")
			}
		})
	}
}
