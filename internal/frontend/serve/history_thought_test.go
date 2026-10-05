package serve

import (
	"testing"

	"reasonix/internal/contract/provider"
)

// A reopened transcript has no live frame to time the thought from, so the
// host's measure has to ride the history the window rebuilds from.
func TestHistoryCarriesTheMeasuredThinkingTime(t *testing.T) {
	got := historyMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "q"},
		{Role: provider.RoleAssistant, Content: "a", ReasoningContent: "r", ThoughtMs: 65800},
	})
	if got[1].ThoughtMs != 65800 || got[1].Reasoning != "r" {
		t.Fatalf("history row = %+v, want the reasoning and its 65800 ms", got[1])
	}
}
