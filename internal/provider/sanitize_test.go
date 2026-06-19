package provider

import "testing"

// --- ApplySanitize pipeline ---

// TestApplySanitizeMatchesLegacy verifies that ApplySanitize (the pipeline
// entry point) produces the same result as SanitizeToolPairing (the backward-
// compatible alias), ensuring the refactor did not change behavior.
func TestApplySanitizeMatchesLegacy(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "go"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Name: "bash", Arguments: `{"cmd":"ls"}`},
			{ID: "c2", Name: "", Arguments: `{}`},
		}},
		{Role: RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok"},
		// c2 is unanswered — should be backfilled.
		{Role: RoleTool, ToolCallID: "orphan", Name: "x", Content: "stray"},
		{Role: RoleUser, Content: "done"},
	}
	pipeline := ApplySanitize(in)
	alias := SanitizeToolPairing(in)
	if len(pipeline) != len(alias) {
		t.Fatalf("ApplySanitize len=%d != SanitizeToolPairing len=%d", len(pipeline), len(alias))
	}
	for i := range pipeline {
		if pipeline[i].Role != alias[i].Role || pipeline[i].Content != alias[i].Content || pipeline[i].ToolCallID != alias[i].ToolCallID {
			t.Errorf("mismatch at %d: pipeline=%+v alias=%+v", i, pipeline[i], alias[i])
		}
	}
}

// TestApplySanitizePipelineIsExtensible verifies that adding a custom step to
// DefaultSanitize is picked up by ApplySanitize.
func TestApplySanitizePipelineIsExtensible(t *testing.T) {
	// Save original pipeline and restore after test.
	orig := DefaultSanitize
	defer func() { DefaultSanitize = orig }()

	// Inject a step that marks every user message.
	DefaultSanitize = []SanitizeOp{
		sanitizeToolPairing,
		func(msgs []Message) []Message {
			out := make([]Message, len(msgs))
			copy(out, msgs)
			for i := range out {
				if out[i].Role == RoleUser {
					out[i].Content = "[sanitized] " + out[i].Content
				}
			}
			return out
		},
	}

	in := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}
	out := ApplySanitize(in)
	if out[0].Content != "[sanitized] hello" {
		t.Errorf("custom step not applied: got %q", out[0].Content)
	}
	// Original must not be mutated.
	if in[0].Content != "hello" {
		t.Errorf("input mutated: %q", in[0].Content)
	}
}

// TestApplySanitizeEmptyInput verifies no panic on empty or nil input.
func TestApplySanitizeEmptyInput(t *testing.T) {
	if out := ApplySanitize(nil); out != nil {
		t.Errorf("nil input: got %v", out)
	}
	if out := ApplySanitize([]Message{}); len(out) != 0 {
		t.Errorf("empty input: got %d messages", len(out))
	}
}
