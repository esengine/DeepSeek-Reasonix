package locktrace_test

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/locktrace"
)

func TestRecorderRetainsItsHoldsAfterReplacement(t *testing.T) {
	first, stop, err := locktrace.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if _, _, err := locktrace.Start(); err == nil {
		t.Fatal("parallel recorder replaced active owner")
	}
	release, err := filelock.Acquire(t.Context(), filepath.Join(t.TempDir(), "actual.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	spans, complete := first.Snapshot()
	if !complete || len(spans) != 1 || spans[0].ReleasedNS != 0 {
		t.Fatalf("live hold not represented: %+v", spans)
	}
	stop()
	second, stopSecond, err := locktrace.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stopSecond)
	release()
	release()
	spans, complete = first.Snapshot()
	if !complete || len(spans) != 1 {
		t.Fatalf("lost original hold: %+v", spans)
	}
	s := spans[0]
	if s.AttemptNS > s.AcquiredNS || s.AcquiredNS > s.ReleasingNS || s.ReleasingNS > s.ReleasedNS || s.ReleasedNS == 0 {
		t.Fatalf("invalid OS boundaries: %+v", s)
	}
	if other, _ := second.Snapshot(); len(other) != 0 {
		t.Fatalf("old release changed successor: %+v", other)
	}
	spans[0].Path = "mutated copy"
	if again, _ := first.Snapshot(); again[0].Path == spans[0].Path {
		t.Fatal("snapshot aliases recorder state")
	}
}
