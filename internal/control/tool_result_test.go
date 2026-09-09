package control

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestCompletionLogSourcesUseStableLocalMessageIdentity(t *testing.T) {
	receipt := &event.CompletionReceipt{Verifications: []event.ReceiptVerification{{ToolCallID: "call"}}}
	messages := []provider.Message{{Role: provider.RoleTool, ID: "entry-one", ToolCallID: "call", Content: "first log"}}
	bound := bindCompletionLogSources(receipt, messages)
	if bound.Verifications[0].ToolResultID != "entry-one" || receipt.Verifications[0].ToolResultID != "" {
		t.Fatal("source missing or executor receipt mutated")
	}
	messages = append(messages, provider.Message{Role: provider.RoleTool, ID: "entry-two", ToolCallID: "call", Content: "later log"})
	if got := bindCompletionLogSources(receipt, messages); got.Verifications[0].ToolResultID != "" {
		t.Fatal("ambiguous provider call ID accepted")
	}
	if bound.Verifications[0].ToolResultID != "entry-one" {
		t.Fatal("later call changed the previously frozen source")
	}
}

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

func TestLookupSearchResultPreservesRecordedMissingSources(t *testing.T) {
	for _, status := range []string{"", provider.SourcesNotProvided} {
		messages := []provider.Message{{Role: provider.RoleAssistant, ServerSearch: []provider.ServerSearchCall{{ID: "s", SourcesStatus: status}}}}
		got := lookupToolResult(messages, "s")
		if got == nil || strings.Contains(got.Output, "not_provided") != (status != "") {
			t.Fatalf("status %q: %+v", status, got)
		}
	}
}
