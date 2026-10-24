package eventwire

import (
	"encoding/json"
	"testing"

	"reasonix/internal/contract/event"
)

func TestWorkspaceLeaseIdentitySurvivesWireAndReplay(t *testing.T) {
	claim := &event.WorkspaceLease{TransactionID: "txn", SessionID: "session", ExecutionID: "child", CallID: "call",
		State: "abandoned", Reason: "timeout", StartedAt: "2026-10-21T00:00:00Z", HolderTransactionID: "held", HolderExecutionID: "other",
		HolderSessionID: "parent", Paths: []string{"a"}, RequestedPaths: []string{"a", "b"}, WaitedMs: 30000}
	data, err := json.Marshal(ToWire(event.Event{Kind: event.Notice, WorkspaceLease: claim}))
	if err != nil {
		t.Fatal(err)
	}
	var replay struct {
		WorkspaceLease *event.WorkspaceLease `json:"workspaceLease"`
	}
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(claim)
	got, _ := json.Marshal(replay.WorkspaceLease)
	if string(got) != string(want) {
		t.Fatalf("lost wait fields: %s != %s", got, want)
	}
	legacy := ToWire(event.Event{Kind: event.Notice, WorkspaceLease: &event.WorkspaceLease{HolderSessionID: "old"}})
	if legacy.WorkspaceLease.TransactionID != "" {
		t.Fatal("invented legacy identity")
	}
}
