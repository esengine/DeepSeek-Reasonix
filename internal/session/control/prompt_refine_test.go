package control

import (
	"testing"

	"reasonix/internal/contract/provider"
)

// What a draft can refer to is what the person and the model said; tool
// traffic and empty assistant turns that only called tools are not that.
func TestConversationTurnsKeepOnlyWhatWasSaid(t *testing.T) {
	got := conversationTurns([]provider.Message{
		{Role: provider.RoleSystem, Content: "system prompt"},
		{Role: provider.RoleUser, Content: "the login loops"},
		{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{{ID: "1"}}},
		{Role: provider.RoleTool, Content: "file contents"},
		{Role: provider.RoleAssistant, Content: "it is the expiry check"},
	})
	if len(got) != 2 || got[0].Role != "user" || got[1].Text != "it is the expiry check" {
		t.Fatalf("turns = %+v", got)
	}
}
