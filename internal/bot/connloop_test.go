package bot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSleepCtxCompletes(t *testing.T) {
	if !SleepCtx(context.Background(), time.Millisecond) {
		t.Fatal("SleepCtx should return true when the full delay elapses")
	}
}

func TestSleepCtxCancelledReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if SleepCtx(ctx, 10*time.Second) {
		t.Fatal("SleepCtx should return false when ctx is cancelled mid-wait")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SleepCtx ignored cancellation: waited %v", elapsed)
	}
}

func TestSleepCtxAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if SleepCtx(ctx, time.Second) {
		t.Fatal("SleepCtx should return false immediately when ctx is already cancelled")
	}
}

func TestNextDelay(t *testing.T) {
	maxD := 30 * time.Second
	cases := []struct{ cur, want time.Duration }{
		{1 * time.Second, 2 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{16 * time.Second, 30 * time.Second}, // doubling past max → capped
		{30 * time.Second, 30 * time.Second}, // stays at max
	}
	for _, c := range cases {
		if got := nextDelay(c.cur, maxD); got != c.want {
			t.Errorf("nextDelay(%v, %v) = %v, want %v", c.cur, maxD, got, c.want)
		}
	}
}

func TestRetryConfigDefaults(t *testing.T) {
	got := RetryConfig{}.withDefaults()
	if got.InitialDelay != defaultInitialDelay || got.MaxDelay != defaultMaxDelay || got.ResetAfter != defaultResetAfter {
		t.Fatalf("zero RetryConfig defaults = %+v", got)
	}
	// MaxDelay below InitialDelay is clamped up to InitialDelay.
	got = RetryConfig{InitialDelay: 5 * time.Second, MaxDelay: time.Second}.withDefaults()
	if got.MaxDelay != 5*time.Second {
		t.Fatalf("MaxDelay should clamp up to InitialDelay, got %v", got.MaxDelay)
	}
}

func TestRunWithRetryNotCalledWhenContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	RunWithRetry(ctx, discardLogger(), "test", RetryConfig{InitialDelay: time.Millisecond}, func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if n := calls.Load(); n != 0 {
		t.Fatalf("attempt ran %d times for an already-cancelled ctx, want 0", n)
	}
}

func TestRunWithRetryRetriesUntilCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		RunWithRetry(ctx, discardLogger(), "test", RetryConfig{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}, func(context.Context) error {
			// Stop the loop from inside the attempt once we've reconnected 3 times.
			if calls.Add(1) >= 3 {
				cancel()
			}
			return errors.New("dropped")
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithRetry did not return after ctx cancellation")
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("attempt ran %d times, want exactly 3", n)
	}
}

func TestRunWithRetryCancelDuringBackoffReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	start := time.Now()
	go func() {
		// Large backoff: the only way this returns quickly is if the backoff wait
		// honors ctx cancellation.
		RunWithRetry(ctx, discardLogger(), "test", RetryConfig{InitialDelay: 10 * time.Second, MaxDelay: 10 * time.Second}, func(context.Context) error {
			return errors.New("dropped")
		})
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithRetry ignored cancellation during backoff")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("RunWithRetry took %v to honor cancellation during backoff", elapsed)
	}
}
