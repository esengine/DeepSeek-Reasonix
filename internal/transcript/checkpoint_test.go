package transcript

import (
	"slices"
	"testing"
)

func TestRestoreCheckpointRepairsLegacyMissingRecordIdentities(t *testing.T) {
	state := Checkpoint{
		Version: ProtocolVersion, Identity: testIdentity, CoveredThroughSeq: 7,
		Records: []Message{
			{Role: "notice", Content: "first"},
			{Role: "notice", Content: "second"},
			{Role: "assistant", MessageID: "answer", Content: "answer"},
			{Role: "tool", ToolCallID: "call", Content: "result"},
		},
	}
	want := []string{"view:checkpoint:7:0", "view:checkpoint:7:1", "m:answer", "tool:call"}
	for attempt := 0; attempt < 2; attempt++ {
		projection, err := RestoreCheckpoint(state, testIdentity)
		if err != nil {
			t.Fatal(err)
		}
		messages := projection.buffer.Messages()
		got := make([]string, len(messages))
		for index := range messages {
			got[index] = messages[index].RecordID
		}
		if !slices.Equal(got, want) {
			t.Fatalf("repaired identities = %v, want %v", got, want)
		}
	}
}

func TestRestoreCheckpointStillRejectsNonEmptyDuplicateIdentity(t *testing.T) {
	state := Checkpoint{Version: ProtocolVersion, Identity: testIdentity, Records: []Message{
		{RecordID: "duplicate", Role: "notice", Content: "first"},
		{RecordID: "duplicate", Role: "notice", Content: "second"},
	}}
	if _, err := RestoreCheckpoint(state, testIdentity); err == nil {
		t.Fatal("duplicate checkpoint identity was accepted")
	}
}
