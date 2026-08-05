package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// blockingReadOnlyTool lets a test park ContractEntries inside the per-tool
// ReadOnly callback at a controlled point.
type blockingReadOnlyTool struct {
	name    string
	entered chan<- struct{}
	release <-chan struct{}
}

func (t *blockingReadOnlyTool) Name() string        { return t.name }
func (t *blockingReadOnlyTool) Description() string { return "blocking test tool" }
func (t *blockingReadOnlyTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *blockingReadOnlyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	return "ok", nil
}
func (t *blockingReadOnlyTool) ReadOnly() bool {
	close(t.entered)
	<-t.release
	return true
}

// TestContractEntriesDoesNotHoldRegistryLockAcrossToolCallbacks is the
// deterministic regression for the AB-BA deadlock fixed in ContractEntries:
// with ContractEntries parked INSIDE a tool's ReadOnly callback, a registry
// writer (Add) must still complete. Under the pre-fix code ContractEntries
// held the registry read lock across ReadOnly, so this interleaving
// deadlocked (lazy MCP placeholders take the spawn mutex in ReadOnly while
// the spawn's trySwap needs the registry write lock).
func TestContractEntriesDoesNotHoldRegistryLockAcrossToolCallbacks(t *testing.T) {
	reg := NewRegistry()
	entered := make(chan struct{})
	release := make(chan struct{})
	reg.Add(&blockingReadOnlyTool{name: "blocking_tool", entered: entered, release: release})

	entriesCh := make(chan []ContractEntry, 1)
	go func() {
		entriesCh <- reg.ContractEntries()
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("ContractEntries never reached the ReadOnly callback")
	}

	// The write must go through while the callback is still parked: the
	// callback must not hold any registry lock.
	addDone := make(chan struct{})
	go func() {
		reg.Add(&blockingReadOnlyTool{name: "writer_tool", entered: make(chan struct{}, 1), release: make(chan struct{})})
		close(addDone)
	}()
	select {
	case <-addDone:
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock: registry writer blocked while ContractEntries was parked inside ReadOnly")
	}

	close(release)
	entries := <-entriesCh
	if len(entries) != 1 || entries[0].Name != "blocking_tool" || !entries[0].ReadOnly {
		t.Fatalf("ContractEntries returned %+v, want one read-only blocking_tool", entries)
	}
}
