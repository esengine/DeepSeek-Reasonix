package plugin

import "testing"

func TestAcquireCodeGraphSlotCapsInstances(t *testing.T) {
	// Drain any residual count from parallel tests by asserting a clean start.
	if got := liveCodeGraphInstances.Load(); got != 0 {
		t.Fatalf("expected 0 live instances at start, got %d", got)
	}

	releases := make([]func(), 0, maxCodeGraphInstances)
	for i := 0; i < maxCodeGraphInstances; i++ {
		release, err := acquireCodeGraphSlot()
		if err != nil {
			t.Fatalf("acquire %d within cap should succeed: %v", i, err)
		}
		releases = append(releases, release)
	}

	// One past the cap must be refused, and the counter must not leak upward.
	if _, err := acquireCodeGraphSlot(); err == nil {
		t.Fatal("acquiring past the cap should fail")
	}
	if got := liveCodeGraphInstances.Load(); got != int32(maxCodeGraphInstances) {
		t.Fatalf("count must stay at cap after a refused acquire, got %d", got)
	}

	// Releasing frees a slot, and release is idempotent.
	releases[0]()
	releases[0]()
	if got := liveCodeGraphInstances.Load(); got != int32(maxCodeGraphInstances-1) {
		t.Fatalf("double-release must free exactly one slot, got %d", got)
	}
	release, err := acquireCodeGraphSlot()
	if err != nil {
		t.Fatalf("a freed slot should be reusable: %v", err)
	}
	releases[0] = release

	for _, r := range releases {
		r()
	}
	if got := liveCodeGraphInstances.Load(); got != 0 {
		t.Fatalf("all slots must be freed at end, got %d", got)
	}
}
