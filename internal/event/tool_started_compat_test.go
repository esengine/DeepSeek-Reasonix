package event

import "testing"

// These values are the persisted event contract before ToolStarted was added
// (origin/main-v2). New events must not reinterpret existing lifecycle records.
func TestToolStartedPreservesPersistedKindValues(t *testing.T) {
	legacy := []struct {
		name  string
		kind  Kind
		value int
	}{
		{"TurnStarted", TurnStarted, 0},
		{"Reasoning", Reasoning, 1},
		{"Text", Text, 2},
		{"Message", Message, 3},
		{"ToolDispatch", ToolDispatch, 4},
		{"ToolResult", ToolResult, 5},
		{"Usage", Usage, 6},
		{"Notice", Notice, 7},
		{"Phase", Phase, 8},
		{"ApprovalRequest", ApprovalRequest, 9},
		{"AskRequest", AskRequest, 10},
		{"TurnDone", TurnDone, 11},
		{"CompactionStarted", CompactionStarted, 12},
		{"CompactionDone", CompactionDone, 13},
		{"ToolProgress", ToolProgress, 14},
		{"MCPSurfaceReady", MCPSurfaceReady, 15},
		{"Retrying", Retrying, 16},
		{"Steer", Steer, 17},
		{"GuardianAssessment", GuardianAssessment, 18},
		{"ExtensionSurface", ExtensionSurface, 19},
		{"ExtensionStatus", ExtensionStatus, 20},
		{"StreamAttempt", StreamAttempt, 21},
		{"ContextMaintenanceEvent", ContextMaintenanceEvent, 22},
		{"WorkspaceChanged", WorkspaceChanged, 23},
		{"TurnPhase", TurnPhase, 24},
		{"CompletionSummary", CompletionSummary, 25},
		{"ToolResultPreview", ToolResultPreview, 26},
		{"TurnStatusChanged", TurnStatusChanged, 27},
		{"PromptAnswered", PromptAnswered, 28},
		{"MCPInteractionRequest", MCPInteractionRequest, 29},
		{"SessionChanged", SessionChanged, 30},
		{"ReadStatus", ReadStatus, 31},
	}
	for _, tc := range legacy {
		if int(tc.kind) != tc.value {
			t.Errorf("persisted %s kind = %d, want %d", tc.name, tc.kind, tc.value)
		}
		if ToolStarted == tc.kind {
			t.Errorf("ToolStarted aliases legacy %s", tc.name)
		}
	}
}
