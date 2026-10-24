package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestFailureHistoryRetainsCauseAcrossSerialization(t *testing.T) {
	const cause = "Image processing failed: configured image understanding model does not accept images"
	message := provider.Message{ID: "failure", Role: provider.RoleTool, LocalOnly: true, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		InterruptedTurn: &provider.InterruptedTurnRecovery{TerminalStatus: "failed", Pending: true, Cause: cause, FailureDiagnostic: &provider.FailureDiagnostic{Kind: provider.FailureKindImageInput}},
	}
	raw, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var reopened provider.Message
	if err := json.Unmarshal(raw, &reopened); err != nil {
		t.Fatal(err)
	}
	rows := History([]provider.Message{reopened}, HistoryOptions{})
	if len(rows) != 1 || rows[0].Code != event.NoticeCodeProviderRequestFailed || rows[0].Content != cause || rows[0].Detail != cause || rows[0].Diagnostic.Kind != provider.FailureKindImageInput {
		t.Fatalf("lost detail: %+v", rows)
	}
	if len(provider.ModelMessages([]provider.Message{reopened})) != 0 {
		t.Fatal("failure detail entered provider context")
	}
	message.InterruptedTurn.Cause = ""
	message.InterruptedTurn.FailureDiagnostic = &provider.FailureDiagnostic{Kind: "unknown"}
	legacy := History([]provider.Message{message}, HistoryOptions{})
	if !strings.Contains(legacy[0].Content, "were not saved") || legacy[0].Detail != "" {
		t.Fatalf("invented legacy cause: %+v", legacy)
	}
}
