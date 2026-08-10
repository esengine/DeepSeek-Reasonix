package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestIssue7935Maintains206ToolResultsOnceAndOnlyProcessesNewTail(t *testing.T) {
	const staleResults = 206
	big := strings.Repeat("line\n", 1_000)
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "first task"},
	}
	for i := range staleResults {
		id := fmt.Sprintf("old-%d", i)
		messages = append(messages,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: big},
		)
	}
	messages = append(messages,
		provider.Message{Role: provider.RoleUser, Content: "recent question"},
		provider.Message{Role: provider.RoleAssistant, Content: "recent answer"},
	)
	sess := &Session{Messages: messages}
	sink := &recordSink{}
	a := New(nil, tool.NewRegistry(), sess, Options{
		ContextWindow: 1_000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, sink)

	// Below the fold trigger nothing may be rewritten: a prefix change voids the
	// prompt cache from that point on, so it waits for the reset it comes with.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 700})
	if got := a.currentProjectionVersion(); got != 0 {
		t.Fatalf("history was rewritten below the fold trigger: projection version %d", got)
	}

	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 850})
	firstVersion := a.currentProjectionVersion()
	if firstVersion == 0 {
		t.Fatal("first maintenance did not install a projection")
	}
	if got := countToolResultsWithPrefix(a.modelVisibleMessages(), prunedMarker); got != staleResults {
		t.Fatalf("first maintenance elided %d results, want %d", got, staleResults)
	}
	status := a.ContextMaintenanceSnapshot()
	if status.ProjectedTokens <= 0 || status.CanonicalTokens <= status.ProjectedTokens {
		t.Fatalf("maintenance snapshot canonical/projected = %d/%d", status.CanonicalTokens, status.ProjectedTokens)
	}
	if status.LastReceipt == nil || status.LastReceipt.Action != "prune" || status.LastReceipt.AffectedToolResults != staleResults {
		t.Fatalf("maintenance snapshot receipt = %+v", status.LastReceipt)
	}
	for i, msg := range sess.Snapshot() {
		if msg.Role == provider.RoleTool && msg.Content != big {
			t.Fatalf("canonical tool result %d was rewritten", i)
		}
	}

	// Already-elided results give the next pass nothing to do; only the new tail
	// is maintained, and the applied-notification count below proves the earlier
	// 206 are not rewritten a second time.
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "new", Name: "read_file", Arguments: "{}"}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "new", Name: "read_file", Content: big})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "move new result out of the protected tail"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 850})
	if got := a.currentProjectionVersion(); got != firstVersion+1 {
		t.Fatalf("new tail maintenance version = %d, want %d", got, firstVersion+1)
	}
	if got := countToolResultsWithPrefix(a.modelVisibleMessages(), prunedMarker); got != staleResults+1 {
		t.Fatalf("maintained visible results = %d, want %d", got, staleResults+1)
	}

	var applied int
	for _, got := range sink.kinds(event.ContextMaintenanceEvent) {
		if got.Maintenance != nil && got.Maintenance.Status == "applied" && got.Maintenance.Action == "prune" {
			applied++
		}
	}
	if applied != 2 {
		t.Fatalf("maintenance notifications = %d, want first pass plus new tail only", applied)
	}
}

func countToolResultsWithPrefix(messages []provider.Message, prefix string) int {
	var count int
	for _, msg := range messages {
		if msg.Role == provider.RoleTool && strings.HasPrefix(msg.Content, prefix) {
			count++
		}
	}
	return count
}
