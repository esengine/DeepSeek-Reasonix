package workspacelease

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func yieldPair(t *testing.T) (a, b *Owner, root string) {
	t.Helper()
	root = t.TempDir()
	locks := t.TempDir()
	var err error
	if a, err = New(root, locks, nil); err != nil {
		t.Fatal(err)
	}
	if b, err = New(root, locks, nil); err != nil {
		t.Fatal(err)
	}
	return a, b, root
}

func within(t *testing.T, o *Owner, paths []string, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return o.AcquirePaths(ctx, paths)
}

func TestYieldLetsAnotherSessionWriteAndResumeQueuesBehindIt(t *testing.T) {
	a, b, root := yieldPair(t)
	file := filepath.Join(root, "x.go")
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatal(err)
	}
	if err := within(t, b, []string{file}, 200*time.Millisecond); err == nil {
		t.Fatal("b wrote a path a still holds")
	}
	resume := a.Yield()
	if a.State().Acquired {
		t.Fatal("a still holds the claim while yielded")
	}
	if err := b.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatalf("b blocked behind a yielded claim: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- resume(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("resume returned %v while b holds the path", err)
	case <-time.After(300 * time.Millisecond):
	}
	b.EndRun()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("resume never got the claim back after b finished")
	}
	if !a.State().Acquired {
		t.Fatal("a does not hold the claim after resuming")
	}
	a.EndRun()
	if a.State().Acquired {
		t.Fatal("claim leaked past the end of the run")
	}
}

func TestYieldResumeFailsWithContextWhenCancelled(t *testing.T) {
	a, b, root := yieldPair(t)
	file := filepath.Join(root, "x.go")
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatal(err)
	}
	resume := a.Yield()
	if err := b.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resume(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("resume = %v, want context.Canceled", err)
	}
	a.EndRun()
	b.EndRun()
	if a.State().Acquired || b.State().Acquired {
		t.Fatal("a claim leaked after cancellation")
	}
}

func TestYieldKeepsClaimWhileACallIsWriting(t *testing.T) {
	a, b, root := yieldPair(t)
	file := filepath.Join(root, "x.go")
	a.BeginRun()
	b.BeginRun()
	end, err := a.HoldPaths(context.Background(), []string{file})
	if err != nil {
		t.Fatal(err)
	}
	resume := a.Yield()
	if !a.State().Acquired {
		t.Fatal("claim was given back under a call that is mid-write")
	}
	if err := within(t, b, []string{file}, 200*time.Millisecond); err == nil {
		t.Fatal("b wrote a path a sibling call is writing")
	}
	if err := resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	end()
	end()
	resume = a.Yield()
	if a.State().Acquired {
		t.Fatal("claim kept after the last writing call ended")
	}
	if err := resume(context.Background()); err != nil || !a.State().Acquired {
		t.Fatalf("resume = %v, acquired = %v", err, a.State().Acquired)
	}
	a.EndRun()
	b.EndRun()
}

func TestYieldWithoutAClaimIsANoOp(t *testing.T) {
	a, _, _ := yieldPair(t)
	a.BeginRun()
	resume := a.Yield()
	if err := resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.State().Acquired {
		t.Fatal("resume took a claim nobody asked for")
	}
	var nilOwner *Owner
	if err := nilOwner.Yield()(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestYieldResumeRestoresTheWholeWorkspaceClaim(t *testing.T) {
	a, b, root := yieldPair(t)
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	resume := a.Yield()
	if err := b.AcquirePaths(context.Background(), []string{filepath.Join(root, "y.go")}); err != nil {
		t.Fatal(err)
	}
	b.EndRun()
	if err := resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.BeginRun()
	if err := within(t, b, []string{filepath.Join(root, "z.go")}, 200*time.Millisecond); err == nil {
		t.Fatal("resumed whole-workspace claim does not exclude other paths")
	}
	b.EndRun()
	a.EndRun()
}

func TestNestedYieldsRetakeTheClaimOnlyWhenTheLastWaiterResumes(t *testing.T) {
	a, b, root := yieldPair(t)
	file := filepath.Join(root, "x.go")
	a.BeginRun()
	b.BeginRun()
	if err := a.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatal(err)
	}
	first := a.Yield()
	second := a.Yield()
	if err := first(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.State().Acquired {
		t.Fatal("claim retaken while another waiter still waits on a person")
	}
	if err := b.AcquirePaths(context.Background(), []string{file}); err != nil {
		t.Fatalf("b blocked while every waiter is yielded: %v", err)
	}
	b.EndRun()
	if err := second(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.State().Acquired {
		t.Fatal("claim not retaken when the last waiter resumed")
	}
	a.EndRun()
}

func TestYieldDoesNotSplitTheAccountOfOneHold(t *testing.T) {
	a, _, root := yieldPair(t)
	var accounts []Stats
	a.OnRelease(func(s Stats) { accounts = append(accounts, s) })
	a.BeginRun()
	if err := a.AcquirePaths(context.Background(), []string{filepath.Join(root, "x.go")}); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := a.Yield()(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(accounts) != 0 {
		t.Fatalf("yields closed %d accounts, want none", len(accounts))
	}
	a.EndRun()
	if len(accounts) != 1 {
		t.Fatalf("accounts after the run = %d, want 1", len(accounts))
	}
}
