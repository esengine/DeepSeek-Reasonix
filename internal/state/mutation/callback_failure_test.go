package mutation

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionPanicReleasesOnlyItsOwnTransaction(t *testing.T) {
	root := t.TempDir()
	c, err := New(root, t.TempDir(), nil, WithContextAdmissionCheck(func(_ context.Context, intent Intent) error {
		if intent.CallID == "panic" {
			panic("fixture admission failure")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	held, err := c.Acquire(t.Context(), Intent{Effect: NamedPaths, Paths: []string{filepath.Join(root, "held")}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(NotExecuted)
	for index, ctx := range []context.Context{t.Context(), held.Context(t.Context())} {
		paths := []string{filepath.Join(root, "held")}
		if index == 0 {
			paths = []string{filepath.Join(root, "other")}
		}
		if lease, err := c.Acquire(ctx, Intent{Effect: NamedPaths, Paths: paths, CallID: "panic"}); lease != nil || !errors.Is(err, ErrCallback) {
			t.Fatalf("admission panic did not fail closed: %v %v", lease, err)
		}
	}
	if err := held.CheckPaths([]string{filepath.Join(root, "held")}); err != nil {
		t.Fatalf("failed child released an existing transaction: %v", err)
	}
	probe, err := c.Acquire(t.Context(), Intent{Effect: NamedPaths, Paths: []string{filepath.Join(root, "other")}})
	if err != nil {
		t.Fatalf("failed admission left resources: %v", err)
	}
	probe.End(NotExecuted)
}

func TestNoticePanicClosesWaitAndReleasesTransaction(t *testing.T) {
	for _, phase := range []string{"waiting", "acquired", "released"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			waiting := make(chan struct{})
			var waitingOnce sync.Once
			var failed atomic.Bool
			var mu sync.Mutex
			var states []string
			c, err := New(root, t.TempDir(), func(notice Notice) {
				if notice.CallID != "target" {
					return
				}
				mu.Lock()
				states = append(states, notice.State)
				mu.Unlock()
				if notice.State == "waiting" {
					waitingOnce.Do(func() { close(waiting) })
				}
				if notice.State == phase && failed.CompareAndSwap(false, true) {
					panic("fixture notice failure")
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			intent := Intent{Effect: NamedPaths, Paths: []string{filepath.Join(root, "shared")}}
			held, err := c.Acquire(t.Context(), intent)
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(NotExecuted)
			type answer struct {
				lease *Lease
				err   error
			}
			done := make(chan answer, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			go func() { intent.CallID = "target"; lease, err := c.Acquire(ctx, intent); done <- answer{lease, err} }()
			select {
			case <-waiting:
			case <-ctx.Done():
				t.Fatal("wait did not open")
			}
			held.End(NotExecuted)
			result := <-done
			if phase == "released" {
				if result.err != nil || result.lease == nil {
					t.Fatalf("acquire failed: %+v", result)
				}
				result.lease.End(Committed)
			} else if result.lease != nil || !errors.Is(result.err, ErrCallback) {
				t.Fatalf("notice failure reached execution: %+v", result)
			}
			if !failed.Load() {
				t.Fatal("fixture did not raise the selected callback failure")
			}
			mu.Lock()
			closed := false
			for _, state := range states {
				closed = closed || state == "abandoned" || (phase == "released" && state == "acquired")
			}
			mu.Unlock()
			if !closed {
				t.Fatal("opened wait never closed")
			}
			intent.CallID = "probe"
			probe, err := c.Acquire(ctx, intent)
			if err != nil {
				t.Fatalf("callback left live resources: %v", err)
			}
			probe.End(NotExecuted)
		})
	}
}
