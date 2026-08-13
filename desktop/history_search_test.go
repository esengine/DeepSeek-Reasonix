package main

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesIncludeServerSearch(t *testing.T) {
	got := historyMessages([]provider.Message{{
		Role:    provider.RoleAssistant,
		Content: "answer only",
		ServerSearch: []provider.ServerSearchCall{{
			ID: "s1", Query: "bitcoin",
			Results: []provider.ServerSearchHit{{Title: "新闻本文", URL: "https://example.com/a"}},
			Raw:     json.RawMessage(`[{"encrypted_content":"xxx"}]`),
		}},
	}}, func(content string) string { return content })
	if len(got) != 1 || got[0].Content != "answer only" {
		t.Fatalf("history = %+v", got)
	}
	if len(got[0].ServerSearch) != 1 || got[0].ServerSearch[0].ID != "s1" || got[0].ServerSearch[0].Query != "bitcoin" {
		t.Fatalf("server search = %+v", got[0].ServerSearch)
	}
	if len(got[0].ServerSearch[0].Raw) != 0 {
		t.Fatalf("history view should omit encrypted replay payload, got %s", got[0].ServerSearch[0].Raw)
	}
}
