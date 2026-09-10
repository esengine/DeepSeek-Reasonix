package provider

import (
	"encoding/json"
	"testing"
)

func TestToolCallRecordPersistsStableIdentityAndRecoveryStates(t *testing.T) {
	record := ToolCallRecord{
		Identity: ActionIdentity{SessionID: "s", TurnID: "t", AttemptID: "a", CallID: "c", CanonicalTool: "write_file", ArgumentDigest: "sha", ResourceScope: "workspace:/tmp/x"},
		State:    ToolRunStarted, ReadOnly: false, IdempotencyKey: "idem", StartedAt: 10,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var got ToolCallRecord
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Identity != record.Identity || got.State != ToolRunStarted || got.IdempotencyKey != "idem" {
		t.Fatalf("record round trip = %+v", got)
	}
}

func TestToolResultRunStatePreservesDurableTerminalStates(t *testing.T) {
	for _, state := range []ToolRunState{ToolRunPending, ToolRunStarted, ToolRunRunning, ToolRunFailed, ToolRunCancelled} {
		if got := ToolResultRunState(Message{ToolRunState: state}); got != state {
			t.Fatalf("state %q classified as %q", state, got)
		}
	}
}
