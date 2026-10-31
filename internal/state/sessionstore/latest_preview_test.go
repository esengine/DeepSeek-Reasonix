package sessionstore

import (
	"testing"

	"reasonix/internal/contract/provider"
)

// A list row says where a conversation stands now — its latest answer or the
// person's latest message — never only how it began.
func TestLatestPreviewIsWhereTheConversationStandsNow(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "create a writer"},
		{Role: provider.RoleAssistant, Content: "Created the writer."},
		{Role: provider.RoleUser, Content: "hand it\nsome   work"},
	}
	if got := LatestPreviewFromMessages(msgs); got != "hand it some work" {
		t.Fatalf("after a message = %q", got)
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "message_agent"}}},
		provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "Delivered."},
		provider.Message{Role: provider.RoleUser, Content: "<host-note>owed</host-note>", HostAuthored: true},
		provider.Message{Role: provider.RoleAssistant, Content: "The writer answered."},
	)
	if got := LatestPreviewFromMessages(msgs); got != "The writer answered." {
		t.Fatalf("after an answer = %q", got)
	}
	if first, _ := SessionPreviewFromMessages(msgs); first != "create a writer" {
		t.Fatalf("the picker's preview moved: %q", first)
	}
}
