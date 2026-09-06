package provider

import (
	"encoding/json"
	"testing"
)

func TestRecordToolRecoveryDistinguishesCancelledAndUnknown(t *testing.T) {
	r := &InterruptedTurnRecovery{}
	RecordToolRecovery(r, InterruptedToolSummary{ID: "cancel", Name: "write_file"}, ToolRunCancelled)
	RecordToolRecovery(r, InterruptedToolSummary{ID: "unknown", Name: "bash"}, ToolRunUnknown)
	if len(r.CancelledTools) != 1 || r.CancelledTools[0].ID != "cancel" {
		t.Fatalf("cancelled tools = %#v", r.CancelledTools)
	}
	if len(r.UnknownTools) != 1 || r.UnknownTools[0].ID != "unknown" {
		t.Fatalf("unknown tools = %#v", r.UnknownTools)
	}
}

func TestToolCallRecordRoundTripsArgumentsLocally(t *testing.T) {
	want := ToolCallRecord{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"git status"}`), State: ToolRunUnknown}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ToolCallRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Arguments) != string(want.Arguments) || got.State != ToolRunUnknown {
		t.Fatalf("round trip = %#v", got)
	}
}
