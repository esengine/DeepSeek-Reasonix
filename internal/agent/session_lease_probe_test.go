package agent

import (
	"path/filepath"
	"testing"
)

// The store-P2 write-authority probe must record an unleased save once per
// path and stay silent for the lease holder — it is evidence collection, not
// enforcement.
func TestUnleasedWriteProbe(t *testing.T) {
	dir := t.TempDir()

	unleased := filepath.Join(dir, "unleased.jsonl")
	observeUnleasedSessionWrite(unleased, sessionSaveSnapshot)
	if _, ok := unleasedWriteObserved.Load(canonicalSessionSavePath(unleased)); !ok {
		t.Fatal("unleased save should be recorded by the probe")
	}

	leased := filepath.Join(dir, "leased.jsonl")
	lease, err := TryAcquireSessionLease(leased)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()
	observeUnleasedSessionWrite(leased, sessionSaveSnapshot)
	if _, ok := unleasedWriteObserved.Load(canonicalSessionSavePath(leased)); ok {
		t.Fatal("lease-holder save must not be recorded by the probe")
	}
}
