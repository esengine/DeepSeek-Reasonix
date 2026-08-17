package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestIngestEventPrintsWebSearchFootnotesAfterAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: "web_search", Output: "Change Log\nhttps://api-docs.deepseek.com/updates/",
	}})
	if len(*m.pendingCommit) != 0 {
		t.Fatalf("search result should stay silent until the answer, committed=%v", *m.pendingCommit)
	}
	m.ingestEvent(event.Event{Kind: event.Text, Text: "answer only"})
	m.ingestEvent(event.Event{Kind: event.Message, Text: "answer only"})
	got := strings.Join(*m.pendingCommit, "\n")
	if !strings.Contains(got, "answer only") || !strings.Contains(got, "Change Log") || !strings.Contains(got, "https://api-docs.deepseek.com/updates/") {
		t.Fatalf("committed=%q, want answer then title/url footnotes", got)
	}
}
