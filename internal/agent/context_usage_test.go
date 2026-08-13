package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// bigSchemaTool mimics a real built-in tool whose JSON schema is large enough
// to matter: the compaction trigger counts tool schemas in the prompt it sizes,
// so the gauge must too.
type bigSchemaTool struct{}

func (bigSchemaTool) Name() string        { return "big_schema" }
func (bigSchemaTool) Description() string { return "a tool with a large schema" }
func (bigSchemaTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"` +
		strings.Repeat("a fairly long property description that consumes tokens. ", 200) +
		`"},"query":{"type":"string","description":"another long field to inflate the schema"}}}`)
}
func (bigSchemaTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (bigSchemaTool) ReadOnly() bool                                           { return true }

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
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 900, CompletionTokens: 100})

	used := a.ContextUsedTokens()
	if got := a.ContextMaintenanceSnapshot().ProjectedTokens; used != got {
		t.Fatalf("gauge = %d, trigger input = %d; they must be the same measurement", used, got)
	}
	if used <= 1_000 {
		t.Fatalf("gauge = %d, want the real view size rather than the last turn's %d", used, 1_000)
	}
}

// Regression: the gauge used the message-only estimator while the trigger also
// sizes tool schemas, so with a non-empty tool registry the gauge under-reported
// the fill — a session could display 80% while the trigger had already crossed
// compact_ratio (and after a compaction the two disagreed again). The gauge must
// call the exact same estimator as the trigger, tool schemas included.
func TestContextUsedTokensIncludesToolSchemasLikeTheTrigger(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(bigSchemaTool{})
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}
	a := New(nil, reg, &Session{Messages: msgs}, Options{
		ContextWindow: 1_000_000,
		RecentKeep:    2,
		ArchiveDir:    t.TempDir(),
	}, event.Discard)

	used := a.ContextUsedTokens()
	// The trigger's own measurement, verbatim.
	if got := a.ContextMaintenanceSnapshot().ProjectedTokens; used != got {
		t.Fatalf("gauge = %d, trigger input = %d; they must be the same measurement", used, got)
	}
	// The gauge must include the tool schemas the trigger counts. The old
	// message-only estimator ignored them and reported exactly the message cost.
	if msgOnly := a.estimatedPromptTokens(a.modelVisibleMessages()); used <= msgOnly {
		t.Fatalf("gauge = %d, message-only estimate = %d; the gauge must price tool schemas like the trigger", used, msgOnly)
	}
}

func TestContextUsedTokensFollowsLiveToolRegistry(t *testing.T) {
	reg := tool.NewRegistry()
	a := New(nil, reg, &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
	}}, Options{ContextWindow: 1_000_000, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	withoutTools := a.ContextUsedTokens()
	reg.Add(bigSchemaTool{})
	withTools := a.ContextUsedTokens()
	if got := a.ContextMaintenanceSnapshot().ProjectedTokens; withTools != got {
		t.Fatalf("gauge after tool registration = %d, trigger input = %d", withTools, got)
	}
	if withTools <= withoutTools {
		t.Fatalf("gauge did not grow after tool registration: %d -> %d", withoutTools, withTools)
	}

	if removed := reg.RemovePrefix("big_"); removed != 1 {
		t.Fatalf("removed %d tools, want 1", removed)
	}
	if got := a.ContextUsedTokens(); got != withoutTools {
		t.Fatalf("gauge after tool removal = %d, want %d", got, withoutTools)
	}

	reg.Add(bigSchemaTool{})
	if got := a.ContextUsedTokens(); got != withTools {
		t.Fatalf("gauge after re-registration = %d, want %d", got, withTools)
	}
	if removed := reg.SuspendPrefix("big_"); removed != 1 {
		t.Fatalf("suspended %d tools, want 1", removed)
	}
	if got := a.ContextUsedTokens(); got != withoutTools {
		t.Fatalf("gauge after tool suspension = %d, want %d", got, withoutTools)
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

	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("more context\n", 500)})
	after := a.ContextUsedTokens()
	if after <= before {
		t.Fatalf("gauge %d -> %d, want the appended turn counted", before, after)
	}
}
