package workspacelease

import (
	"context"
	"testing"
	"time"
)

func statsOwner(t *testing.T) (*Owner, <-chan Stats) {
	t.Helper()
	closed := make(chan Stats, 4)
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.OnRelease(func(s Stats) { closed <- s })
	return owner, closed
}

func TestAnUncontendedHoldStillReports(t *testing.T) {
	owner, closed := statsOwner(t)
	owner.BeginRun()
	if err := owner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Windows' monotonic clock advances about every 15ms, so an instant hold
	// reads as zero there and says nothing about whether it was accounted for.
	time.Sleep(40 * time.Millisecond)
	owner.EndRun()

	got := <-closed
	if got.Contended != 0 || got.Reported != 0 || got.Waited != 0 {
		t.Fatalf("an uncontended hold reported contention: %+v", got)
	}
	if got.Held < 30*time.Millisecond {
		t.Fatalf("held did not span the hold: %+v", got)
	}
}

// The rate this instrument exists to measure needs a denominator, and a session
// that never waited is most of it.
func TestIdleIsTheHoldAfterTheLastWriteAsked(t *testing.T) {
	owner, closed := statsOwner(t)
	owner.BeginRun()
	if err := owner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	owner.EndRun()

	got := <-closed
	if got.Idle < 30*time.Millisecond {
		t.Fatalf("idle did not span the stretch after the last ask: %+v", got)
	}
	if got.Idle > got.Held {
		t.Fatalf("idle outran the hold it is part of: %+v", got)
	}
}

// A second ask re-dates the last write, so a session that keeps writing to the
// end reports no idle stretch at all.
func TestAskingAgainResetsTheIdleStretch(t *testing.T) {
	owner, closed := statsOwner(t)
	owner.BeginRun()
	if err := owner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if err := owner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner.EndRun()

	got := <-closed
	if got.Idle > 20*time.Millisecond {
		t.Fatalf("a later ask did not re-date the last write: %+v", got)
	}
}

// A wait that clears inside the grace never becomes a notice, which is exactly
// why the count cannot be taken from the notices.
func TestAWaitUnderTheGraceIsCountedButNotReported(t *testing.T) {
	restore := waitNoticeGrace
	waitNoticeGrace = time.Hour
	defer func() { waitNoticeGrace = restore }()

	root, lockDir := t.TempDir(), t.TempDir()
	holder, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	holder.BeginRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}

	closed := make(chan Stats, 1)
	waiter, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	waiter.OnRelease(func(s Stats) { closed <- s })
	waiter.BeginRun()

	acquired := make(chan error, 1)
	go func() { acquired <- waiter.AcquireWrite(context.Background()) }()
	time.Sleep(60 * time.Millisecond)
	holder.EndRun()
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	waiter.EndRun()

	got := <-closed
	if got.Contended != 1 {
		t.Fatalf("the wait was not counted: %+v", got)
	}
	if got.Reported != 0 {
		t.Fatalf("a wait inside the grace was reported: %+v", got)
	}
	if got.Waited <= 0 {
		t.Fatalf("the wait had no length: %+v", got)
	}
}

func TestAnOwnerWithNoNoticeKeepsNoAccount(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	if err := owner.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner.EndRun()
	if got := owner.State(); got.Acquired || got.Waiting {
		t.Fatalf("the lease outlived its run: %+v", got)
	}
}
