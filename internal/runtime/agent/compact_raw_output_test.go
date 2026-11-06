package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

const rawOnlySentinel = "RAW-ONLY-SENTINEL"

// A tool result whose Content was bounded when the model first saw it, with the
// full output kept beside it for local display and recall.
func boundedToolResult(rawCopies int) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"big.log"}`}}},
		{
			Role: provider.RoleTool, ToolCallID: "1", Name: "read_file",
			Content:    "bounded head\n[output truncated]\n",
			RawContent: "bounded head\n" + strings.Repeat(rawOnlySentinel+" filler line\n", rawCopies),
		},
	}
}

// Recall is the deliberate exception and stays one: it answers with what was
// actually run, as a tool result the recall budget bounds.
func TestRecallStillAnswersFromTheFullOutput(t *testing.T) {
	rendered := renderTranscriptVerbatim(boundedToolResult(3))
	if !strings.Contains(rendered, rawOnlySentinel) {
		t.Error("recall lost the full output it exists to return")
	}
}
