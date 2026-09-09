package serve

import (
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesHidePinnedContextRevisions(t *testing.T) {
	got := historyMessages([]provider.Message{
		{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "<pinned_context_revision>private pinned body</pinned_context_revision>"},
		{Role: provider.RoleUser, Content: "visible question"},
	})
	if len(got) != 1 || got[0].Content != "visible question" {
		t.Fatalf("historyMessages() = %#v, want only visible question", got)
	}
}
