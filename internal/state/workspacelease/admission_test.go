package workspacelease

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func withWriteAdmission(check func([]string) error) Option {
	return func(o *Owner) { o.AddWriteAdmission("initial", check) }
}

func TestWriteAdmissionCoversRetainedAndOptimisticallyWaivedClaims(t *testing.T) {
	for _, optimistic := range []bool{false, true} {
		t.Run(map[bool]string{false: "strict", true: "optimistic"}[optimistic], func(t *testing.T) {
			var blocked atomic.Bool
			refused := errors.New("workspace needs recovery")
			opts := []Option{withWriteAdmission(func([]string) error {
				if blocked.Load() {
					return refused
				}
				return nil
			})}
			if optimistic {
				opts = append(opts, WithoutWholeWorkspaceHold())
			}
			owner, err := New(t.TempDir(), t.TempDir(), nil, opts...)
			if err != nil {
				t.Fatal(err)
			}
			owner.BeginRun()
			defer owner.EndRun()
			if err := owner.AcquirePaths(t.Context(), []string{"a"}); err != nil {
				t.Fatal(err)
			}
			blocked.Store(true)
			for _, paths := range [][]string{{"a"}, nil, {"b"}} {
				if _, _, err := owner.HoldPaths(t.Context(), paths); !errors.Is(err, refused) {
					t.Fatalf("barrier bypass: %v", err)
				}
			}
		})
	}
}

func TestWriteAdmissionRebuildRetainsAndExtendsResourceDomains(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil, WithoutWholeWorkspaceHold())
	if err != nil {
		t.Fatal(err)
	}
	oldBlocked, newBlocked := atomic.Bool{}, atomic.Bool{}
	oldError, newError := errors.New("old recovery"), errors.New("new recovery")
	owner.AddWriteAdmission("old", func([]string) error {
		if oldBlocked.Load() {
			return oldError
		}
		return nil
	})
	owner.BeginRun()
	defer owner.EndRun()
	if err := owner.AcquirePaths(t.Context(), []string{"old"}); err != nil {
		t.Fatal(err)
	}
	owner.AddWriteAdmission("new", func([]string) error {
		if newBlocked.Load() {
			return newError
		}
		return nil
	})
	newBlocked.Store(true)
	if _, _, err := owner.HoldPaths(t.Context(), nil); !errors.Is(err, newError) {
		t.Fatalf("new domain omitted: %v", err)
	}
	newBlocked.Store(false)
	oldBlocked.Store(true)
	if _, _, err := owner.HoldPaths(t.Context(), []string{"old"}); !errors.Is(err, oldError) {
		t.Fatalf("old domain removed: %v", err)
	}
	owner.AddWriteAdmission("old", func([]string) error { return nil })
	if _, _, err := owner.HoldPaths(t.Context(), nil); !errors.Is(err, oldError) {
		t.Fatalf("rebuild replaced existing domain: %v", err)
	}
}

func TestWriteAdmissionRechecksQueuedClaimBeforePublishingAndClosesWait(t *testing.T) {
	oldGrace := waitNoticeGrace
	waitNoticeGrace = 0
	defer func() { waitNoticeGrace = oldGrace }()
	root, state := t.TempDir(), t.TempDir()
	holder, err := New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	holder.BeginRun()
	defer holder.EndRun()
	if err := holder.AcquirePaths(t.Context(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	var blocked atomic.Bool
	waiting, abandoned := make(chan struct{}, 1), make(chan struct{}, 1)
	refused := errors.New("journal appeared while waiting")
	owner, err := New(root, state, func(w Wait) {
		switch w.Outcome {
		case WaitBegan:
			waiting <- struct{}{}
		case WaitAbandoned:
			abandoned <- struct{}{}
		}
	}, withWriteAdmission(func([]string) error {
		if blocked.Load() {
			return refused
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- owner.AcquirePaths(ctx, []string{"a"}) }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	blocked.Store(true)
	holder.EndRun()
	if err := <-done; !errors.Is(err, refused) {
		t.Fatal(err)
	}
	select {
	case <-abandoned:
	case <-ctx.Done():
		t.Fatal("open wait was not closed")
	}
	other, err := New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	other.BeginRun()
	defer other.EndRun()
	if err := other.AcquirePaths(ctx, []string{"a"}); err != nil {
		t.Fatalf("refused claimant retained new occupancy: %v", err)
	}
}
