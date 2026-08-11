package agent

import (
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func usageFixture(t *testing.T, toolResults int) *Agent {
	t.Helper()
	big := strings.Repeat("line\n", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}
	for i := range toolResults {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}},
			provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file", Content: big},
		)
	}
	return New(nil, tool.NewRegistry(), &Session{Messages: msgs}, Options{
		ContextWindow: 1_000_000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)
}

// The gauge and the compaction trigger must read the same number. Feeding the
// gauge from the last turn's provider usage let a session report 8% while it
// was compacting: that number lags a turn, counts completion tokens the trigger
// never looks at, and is zero until the first turn of a rebound session.
func TestContextUsedTokensMatchesTheTriggerInput(t *testing.T) {
	a := usageFixture(t, 12)
	// A stale, tiny reading from the previous turn — exactly what a fold leaves
	// behind, and what the gauge used to display.
	a.lastUsage.Store(&provider.Usage{PromptTokens: 900, CompletionTokens: 100})

	used := a.ContextUsedTokens()
	if got := a.ContextMaintenanceSnapshot().ProjectedTokens; used != got {
		t.Fatalf("gauge = %d, trigger input = %d; they must be the same measurement", used, got)
	}
	if used <= 1_000 {
		t.Fatalf("gauge = %d, want the real view size rather than the last turn's %d", used, 1_000)
	}
}

func TestContextUsedTokensIsZeroWithoutASession(t *testing.T) {
	a := &Agent{}
	if got := a.ContextUsedTokens(); got != 0 {
		t.Fatalf("gauge without a session = %d, want 0 so the frontend hides it", got)
	}
}

func TestContextUsedTokensFollowsTheTranscript(t *testing.T) {
	a := usageFixture(t, 4)
	before := a.ContextUsedTokens()
	if before != a.ContextUsedTokens() {
		t.Fatal("repeated reads of an unchanged view disagreed")
	}

	a.session.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("more context\n", 500)})
	after := a.ContextUsedTokens()
	if after <= before {
		t.Fatalf("gauge %d -> %d, want the appended turn counted", before, after)
	}
}
