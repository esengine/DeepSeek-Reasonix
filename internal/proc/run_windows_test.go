//go:build windows

package proc

import "testing"

func TestSetTreeRetainsFrozenIdentityRetryAfterEarlyJoblessKill(t *testing.T) {
	tracked := &TrackedCommand{killed: true, jobBacked: false}
	tree := &TreeTracker{
		done:    make(chan struct{}),
		records: map[uint32]processRecord{},
	}

	tracked.setTree(tree)
	if tracked.tree != tree {
		t.Fatal("early jobless cancellation discarded the fixed-identity retry tracker")
	}
	tree.mu.Lock()
	frozen := tree.frozen
	tree.mu.Unlock()
	if !frozen {
		t.Fatal("early jobless cancellation did not freeze the tracker identity set")
	}
}

func TestFinishTrackingFreezesJoblessIdentitySet(t *testing.T) {
	tree := &TreeTracker{
		done:    make(chan struct{}),
		records: map[uint32]processRecord{},
	}
	tracked := &TrackedCommand{tree: tree, jobBacked: false}

	tracked.finishTracking()

	if tracked.tree != nil {
		t.Fatal("finishTracking retained the jobless TreeTracker")
	}
	tree.mu.Lock()
	frozen := tree.frozen
	tree.mu.Unlock()
	if !frozen {
		t.Fatal("normal root exit stopped the jobless tracker without freezing its recorded identities")
	}
}
