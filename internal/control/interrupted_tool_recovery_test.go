package control

import (
	"testing"

	"reasonix/internal/provider"
)

func TestInterruptedRecoveryPrefersLedgerBarrierOverPlaceholder(t *testing.T) {
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "write-1", Name: "write_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "write-1", Name: "write_file", Content: "[no result: the previous turn was interrupted before this tool call completed]"},
	}
	evidence := &interruptedTailEvidence{turnID: "turn-1", states: map[string]provider.ToolRunState{"write-1": provider.ToolRunUnknown}}
	recordInterruptedAssistantRecovery(recovery, msgs, 0, evidence)
	if len(recovery.UnknownTools) != 1 || !recovery.RequiresUserDecision {
		t.Fatalf("recovery=%+v, want one unknown call requiring user decision", recovery)
	}
	if len(recovery.ToolCalls) != 1 || recovery.ToolCalls[0].State != provider.ToolRunUnknown {
		t.Fatalf("tool records=%+v, want ledger-proven unknown state", recovery.ToolCalls)
	}
}

func TestInterruptedRecoveryUsesCancelledForUnstartedLedgerCall(t *testing.T) {
	recovery := &provider.InterruptedTurnRecovery{Pending: true}
	msgs := []provider.Message{{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{}`}}}, {Role: provider.RoleTool, ToolCallID: "read-1", Name: "read_file", Content: "[no result: the previous turn was interrupted before this tool call completed]"}}
	evidence := &interruptedTailEvidence{turnID: "turn-2", states: map[string]provider.ToolRunState{"read-1": provider.ToolRunCancelled}}
	recordInterruptedAssistantRecovery(recovery, msgs, 0, evidence)
	if len(recovery.NotStartedTools) != 1 || len(recovery.UnknownTools) != 0 {
		t.Fatalf("recovery=%+v, want cancelled call outside unknown bucket", recovery)
	}
}
