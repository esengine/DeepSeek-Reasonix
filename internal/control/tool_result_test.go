package control

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestLookupToolResultFindsServerSearch(t *testing.T) {
	msgs := []provider.Message{{
		Role:    provider.RoleAssistant,
		Content: "answer only",
		ServerSearch: []provider.ServerSearchCall{{
			ID:      "s1",
			Query:   "bitcoin",
			Results: []provider.ServerSearchHit{{Title: "新闻本文", URL: "https://example.com/a"}},
		}},
	}}
	got := lookupToolResult(msgs, "s1")
	if got == nil || got.Args != `{"query":"bitcoin"}` || !strings.Contains(got.Output, "新闻本文") {
		t.Fatalf("lookup = %#v", got)
	}
	if lookupToolResult(msgs, "missing") != nil {
		t.Fatal("unknown tool id should miss")
	}
}
