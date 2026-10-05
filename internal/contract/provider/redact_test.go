package provider

import (
	"strings"
	"testing"
)

func TestRedactMessagesDoesNotMutateInput(t *testing.T) {
	const secret = "sk-real-secret-value-123456"
	msgs := []Message{
		{
			Role:    RoleAssistant,
			Content: "checking",
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "bash", Arguments: `{"command":"echo DEEPSEEK_API_KEY=` + secret + `"}`},
			},
			MemoryCitations: []MemoryCitation{{Note: "token " + secret}},
		},
		{Role: RoleTool, ToolCallID: "call_1", Content: "DEEPSEEK_API_KEY=" + secret},
	}

	out := RedactMessages(msgs)

	// The redacted copy must not carry the raw secret...
	if strings.Contains(out[0].ToolCalls[0].Arguments, secret) || strings.Contains(out[1].Content, secret) {
		t.Fatalf("redacted copy leaked secret: %+v", out)
	}
	// ...and the input — live session history the model still replays — must
	// be untouched, including through the shared ToolCalls/MemoryCitations
	// backing arrays.
	if !strings.Contains(msgs[0].ToolCalls[0].Arguments, secret) {
		t.Fatalf("RedactMessages mutated the caller's ToolCalls: %q", msgs[0].ToolCalls[0].Arguments)
	}
	if !strings.Contains(msgs[0].MemoryCitations[0].Note, secret) {
		t.Fatalf("RedactMessages mutated the caller's MemoryCitations: %q", msgs[0].MemoryCitations[0].Note)
	}
	if !strings.Contains(msgs[1].Content, secret) {
		t.Fatalf("RedactMessages mutated the caller's Content: %q", msgs[1].Content)
	}
}
