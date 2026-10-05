package telemetry

import (
	"testing"

	"reasonix/internal/contract/event"
)

func has(t *testing.T, counts map[string]int, signal, bucket string) {
	t.Helper()
	if counts[signal+"\x00"+bucket] == 0 {
		t.Fatalf("expected %s=%s in %v", signal, bucket, counts)
	}
}

func absent(t *testing.T, counts map[string]int, signal string) {
	t.Helper()
	for key := range counts {
		if len(key) > len(signal) && key[:len(signal)] == signal {
			t.Fatalf("expected no %s in %v", signal, counts)
		}
	}
}

// The denominator is every hold. A session that wrote and never waited is most
// of what the contention rate is a rate of, so it has to be counted.
func TestAnUncontendedHoldIsStillReported(t *testing.T) {
	counts := leaseCounts(&event.WorkspaceLease{HeldMs: 400, IdleMs: 380})
	has(t, counts, "lease_hold", "uncontended")
	has(t, counts, "lease_idle", "pc_75_100")
	absent(t, counts, "lease_wait")
}

func TestAContendedHoldCarriesItsWait(t *testing.T) {
	counts := leaseCounts(&event.WorkspaceLease{Contended: 2, WaitedMs: 3000, HeldMs: 1000, IdleMs: 100})
	has(t, counts, "lease_hold", "contended")
	has(t, counts, "lease_wait", "s_1_5")
	has(t, counts, "lease_idle", "lt_25")
}

// A hold with no length has no share, and a bucket of "lt_25" for one would
// read as a hold measured to have been busy throughout.
func TestAZeroLengthHoldHasNoShare(t *testing.T) {
	counts := leaseCounts(&event.WorkspaceLease{HeldMs: 0, IdleMs: 0})
	has(t, counts, "lease_hold", "uncontended")
	absent(t, counts, "lease_idle")
}

func TestNoAccountCountsNothing(t *testing.T) {
	if got := leaseCounts(nil); len(got) != 0 {
		t.Fatalf("a missing account produced counters: %v", got)
	}
}

// Nothing leaves the process unless its name is on the list.
func TestEveryLeaseSignalIsAllowedToUpload(t *testing.T) {
	for _, signal := range []string{"lease_hold", "lease_wait", "lease_idle"} {
		if !uploadSignals[signal] {
			t.Fatalf("%s is emitted and cannot be uploaded", signal)
		}
	}
}

func TestShareBucketsCoverTheRange(t *testing.T) {
	for _, tc := range []struct {
		share float64
		want  string
	}{{0, "lt_25"}, {0.24, "lt_25"}, {0.25, "pc_25_50"}, {0.5, "pc_50_75"}, {0.75, "pc_75_100"}, {1, "pc_75_100"}} {
		if got := shareBucket(tc.share); got != tc.want {
			t.Fatalf("shareBucket(%v) = %q, want %q", tc.share, got, tc.want)
		}
	}
}
