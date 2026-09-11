package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestEstimateMessagesTokensOmitsEncryptedSearchRaw(t *testing.T) {
	visible := provider.ServerSearchCall{
		ID: "s1", Query: "q",
		Results: []provider.ServerSearchHit{{Title: "T", URL: "https://a.example"}},
	}
	withRaw := []provider.Message{{
		Role: provider.RoleAssistant, Content: "answer",
		ServerSearch: []provider.ServerSearchCall{{
			ID: visible.ID, Query: visible.Query, Results: visible.Results,
			Raw: json.RawMessage(strings.Repeat("E", 50_000)),
		}},
	}}
	withoutRaw := []provider.Message{{
		Role:         provider.RoleAssistant,
		Content:      "answer",
		ServerSearch: []provider.ServerSearchCall{visible},
	}}
	if got, want := estimateMessagesTokens(withRaw), estimateMessagesTokens(withoutRaw); got != want {
		t.Fatalf("estimateMessagesTokens with raw = %d, without = %d", got, want)
	}
	if estimateSamplingRequestInputTokens(provider.Request{Messages: withRaw}) != estimateSamplingRequestInputTokens(provider.Request{Messages: withoutRaw}) {
		t.Fatal("interrupted-usage estimate counted encrypted search raw")
	}
}
