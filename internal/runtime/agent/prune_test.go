package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestMaintenanceUsesSummaryNotPruneAtFoldTrigger(t *testing.T) {
	big := strings.Repeat("tool body line\n", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}
	for i := range 12 {
		id := "t" + string(rune('a'+i))
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: big},
		)
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleUser, Content: "tail"},
		provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	)
	prov := &countingProvider{reply: "digest"}
	a := New(prov, tool.NewRegistry(), &sessionstore.Session{Messages: msgs}, Options{
		ContextWindow: 20_000, CompactRatio: 0.5, RecentKeep: 2,
	}, event.Discard)
	if _, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: CompactionTriggerPressure}); err != nil {
		t.Fatal(err)
	}
	if a.window().currentProjectionVersion() != 1 {
		t.Fatalf("projection version = %d, want 1", a.window().currentProjectionVersion())
	}
	if len(prov.got) != 1 {
		t.Fatalf("summarizer calls = %d, want 1", len(prov.got))
	}
}

func TestSnipStrategyStillAvailableForFirstVisibleAndSummaryInput(t *testing.T) {
	a := &Agent{svc: agentServices{tools: tool.NewRegistry()}}
	s := a.window().snipStrategyFor("read_file")
	if s.head <= 0 || s.tail <= 0 {
		t.Fatalf("snip strategy for read_file = %+v", s)
	}
	body, notice := truncateToolOutputFor(strings.Repeat("x", MaxToolOutputBytes+100), "read_file", "call-1", MaxToolOutputBytes, s)
	if notice == "" || !strings.Contains(body, "call_id=call-1") {
		t.Fatalf("first-visible truncation missing marker: notice=%q body=%.200q", notice, body)
	}
	if len(body) > MaxToolOutputBytes+200 {
		t.Fatalf("bounded body still oversized: %d", len(body))
	}
}
