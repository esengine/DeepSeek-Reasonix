package eventwire

import (
	"encoding/json"
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"testing"
)

func TestTurnResultWireRoundTrip(t *testing.T) {
	turn, exitCode := 0, 1
	in := event.Event{Kind: event.TurnDone, CheckpointTurn: &turn, Receipt: &event.CompletionReceipt{
		Verdict: "partial", Diff: &checkpoint.TurnChanges{ID: "result-1", Turn: 0, Coverage: "partial", Files: []checkpoint.TurnFile{}, Reasons: []string{"active_writer"}},
		Verifications: []event.ReceiptVerification{{Command: "go test ./...", ToolCallID: "call-1", ToolResultID: "entry-1", ExitCode: &exitCode, Interrupted: true}},
	}}
	b, err := json.Marshal(ToWire(in))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	receipt := CompletionReceiptEvent(decoded.Receipt)
	if receipt.Verifications[0].ToolResultID != "entry-1" {
		t.Fatal("stable log source lost")
	}
	if decoded.CheckpointTurn == nil || *decoded.CheckpointTurn != 0 || receipt.Diff.ID != "result-1" || !receipt.Verifications[0].Interrupted || receipt.Verifications[0].ToolCallID != "call-1" || *receipt.Verifications[0].ExitCode != 1 {
		t.Fatalf("roundtrip: %s", b)
	}
	var old struct {
		Kind    string `json:"kind"`
		Receipt struct {
			Verdict string `json:"verdict"`
		} `json:"receipt"`
	}
	if err := json.Unmarshal(b, &old); err != nil || old.Kind != "turn_done" || old.Receipt.Verdict != "partial" {
		t.Fatalf("old reader: %+v %v", old, err)
	}
}

func TestVerificationProgressIsHostMetadata(t *testing.T) {
	wire := ToWire(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "check", Verifying: true}})
	if wire.Tool == nil || !wire.Tool.Verifying || wire.Tool.Output != "" {
		t.Fatalf("progress: %+v", wire)
	}
}
