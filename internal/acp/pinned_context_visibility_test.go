package acp

import (
	"testing"

	"reasonix/internal/provider"
)

func TestTitleFromHistorySkipsPinnedContextRevision(t *testing.T) {
	history := []provider.Message{
		{Role: provider.RoleUser, Origin: provider.MessageOriginHost, Content: "<pinned_context_revision>private pinned body</pinned_context_revision>"},
		{Role: provider.RoleUser, Content: "visible question"},
	}
	if got := titleFromHistory(history); got != "visible question" {
		t.Fatalf("titleFromHistory() = %q, want visible question", got)
	}
}
