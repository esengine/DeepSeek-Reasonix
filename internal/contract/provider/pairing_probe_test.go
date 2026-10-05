package provider

import "testing"

// TestSanitizeDuplicateToolCallIDsKeepsEachResult probes whether a malformed
// history with two tool calls sharing one id round-trips both results. The map
// keyed on id collapses them, so the loop guard's safety net silently drops one.
func TestSanitizeDuplicateToolCallIDsKeepsEachResult(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "dup", Name: "read_file"},
			{ID: "dup", Name: "grep"},
		}},
		{Role: RoleTool, ToolCallID: "dup", Name: "read_file", Content: "FILE-RESULT"},
		{Role: RoleTool, ToolCallID: "dup", Name: "grep", Content: "GREP-RESULT"},
	}
	out := SanitizeToolPairing(msgs)

	var got []string
	for _, m := range out {
		if m.Role == RoleTool {
			got = append(got, m.Content)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want 2 tool results, got %d: %v", len(got), got)
	}
	if got[0] == got[1] {
		t.Errorf("both tool results collapsed to the same content %q — one was lost", got[0])
	}
}

// TestSanitizeEmptyToolCallIDs probes two calls with empty ids — same collapse
// risk, and the placeholder/backfill path keys on "" too.
func TestSanitizeEmptyToolCallIDsKeepsEachResult(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "", Name: "a"},
			{ID: "", Name: "b"},
		}},
		{Role: RoleTool, ToolCallID: "", Name: "a", Content: "RESULT-A"},
		{Role: RoleTool, ToolCallID: "", Name: "b", Content: "RESULT-B"},
	}
	out := SanitizeToolPairing(msgs)

	var got []string
	for _, m := range out {
		if m.Role == RoleTool {
			got = append(got, m.Content)
		}
	}
	if len(got) != 2 || got[0] == got[1] {
		t.Errorf("empty-id pair collapsed: %v", got)
	}
}
