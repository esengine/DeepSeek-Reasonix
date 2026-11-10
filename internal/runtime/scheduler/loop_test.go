package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/testenv"
)

func runLoop(t *testing.T, l *Loop, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), d)
	defer cancel()
	l.Every = 5 * time.Millisecond
	l.Run(ctx)
}

func TestALoopNeverTicksWhileIneligible(t *testing.T) {
	var ticks atomic.Int32
	l := &Loop{
		Tick:     func(context.Context) (Result, error) { ticks.Add(1); return Result{}, nil },
		Eligible: func() bool { return false },
		Lock:     func() (func(), error) { return func() {}, nil },
	}
	runLoop(t, l, 80*time.Millisecond)
	if ticks.Load() != 0 {
		t.Fatalf("ticked %d times with no supervised lifetime or visible surface", ticks.Load())
	}
}

func TestALoopStandsByWhileAnotherProcessHoldsTheLock(t *testing.T) {
	var ticks atomic.Int32
	l := &Loop{
		Tick:     func(context.Context) (Result, error) { ticks.Add(1); return Result{}, nil },
		Eligible: func() bool { return true },
		Lock:     func() (func(), error) { return nil, errors.New("held") },
	}
	runLoop(t, l, 80*time.Millisecond)
	if ticks.Load() != 0 {
		t.Fatalf("ticked %d times without the ticker lock", ticks.Load())
	}
}

func TestALoopTicksUnderTheLockAndReleasesIt(t *testing.T) {
	var held, maxHeld, ticks, released atomic.Int32
	seen := make(chan struct{}, 8)
	l := &Loop{
		Tick: func(context.Context) (Result, error) {
			ticks.Add(1)
			if n := held.Load(); n > maxHeld.Load() {
				maxHeld.Store(n)
			}
			return Result{}, nil
		},
		Eligible: func() bool { return true },
		Lock: func() (func(), error) {
			held.Add(1)
			return func() { held.Add(-1); released.Add(1) }, nil
		},
		Done: func(Result, error) {
			select {
			case seen <- struct{}{}:
			default:
			}
		},
	}
	runLoop(t, l, 120*time.Millisecond)
	if ticks.Load() == 0 || len(seen) == 0 || maxHeld.Load() != 1 || held.Load() != 0 || released.Load() != ticks.Load() {
		t.Fatalf("ticks=%d held=%d max=%d released=%d", ticks.Load(), held.Load(), maxHeld.Load(), released.Load())
	}
}

// Two kernels on one machine: the OS lock lets one tick at a time.
func TestTwoLoopsOnOneLockFileNeverTickTogether(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "scheduler.lock")
	var inside, overlap, ticks atomic.Int32
	mk := func() *Loop {
		return &Loop{
			Tick: func(context.Context) (Result, error) {
				if inside.Add(1) > 1 {
					overlap.Add(1)
				}
				ticks.Add(1)
				time.Sleep(12 * time.Millisecond)
				inside.Add(-1)
				return Result{}, nil
			},
			Eligible: func() bool { return true },
			Lock:     func() (func(), error) { return filelock.TryAcquire(path) },
		}
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { runLoop(t, mk(), 150*time.Millisecond) })
	}
	wg.Wait()
	if ticks.Load() == 0 || overlap.Load() != 0 {
		t.Fatalf("ticks=%d overlapping ticks=%d", ticks.Load(), overlap.Load())
	}
}
