package sessionstore

import (
	"reasonix/internal/base/testenv"
	"sync"
	"testing"
	"time"
)

// A cold folder is a wait on the disk. The sidecars are read by a few readers
// at once, never more than the bound, and the listing is the one a single
// reader would have produced.
func TestAColdFolderIsReadByABoundedPoolAndListsTheSame(t *testing.T) {
	dir := testenv.TempDir(t)
	writeSessions(t, dir, 200)

	var mu sync.Mutex
	inFlight, peak := 0, 0
	branchMetaReadHook = func(string) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
	}
	t.Cleanup(func() { branchMetaReadHook = nil })

	got := list(t, dir)
	if peak < 2 || peak > orderReaders {
		t.Fatalf("peak concurrent sidecar reads = %d, want between 2 and %d", peak, orderReaders)
	}
	if len(got) != 200 {
		t.Fatalf("listed %d sessions, want 200", len(got))
	}

	branchMetaReadHook = nil
	again := list(t, dir)
	if len(again) != len(got) {
		t.Fatalf("a warm listing has %d rows, the cold one %d", len(again), len(got))
	}
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("row %d differs between the cold and the remembered listing:\n%+v\n%+v", i, got[i], again[i])
		}
	}
}
