package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// A retry aggregate is billable tokens, not context shape. Reporting it would
// show pressure that the model never actually saw.
func TestContextReportUsesLatestPromptNotBillableAggregate(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 400)})
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 100_000, RecentKeep: 2,
	}, event.Discard)

	a.sess.output.lastUsage.Store(&provider.Usage{
		PromptTokens:        363_000, // three recovery attempts, billed
		ContextPromptTokens: 122_000, // what the last request actually carried
		RequestCount:        3,
	})

	if got := a.ContextReport().LatestPrompt; got != 122_000 {
		t.Errorf("LatestPrompt = %d, want 122000 (the latest request, not the billed aggregate)", got)
	}
}

// The sole trigger must come from the same helper the decision uses.
func TestContextReportThresholdsMatchTheDecision(t *testing.T) {
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), NewSession("sys"), Options{
		ContextWindow: 200_000, CompactRatio: 0.85, RecentKeep: 2,
	}, event.Discard)

	rep := a.ContextReport()
	fold := a.compactTrigger()
	if rep.FoldThreshold != fold {
		t.Errorf("report FoldThreshold %d differs from compactTrigger %d", rep.FoldThreshold, fold)
	}
	if rep.SoftThreshold != 0 || rep.SnipThreshold != 0 || rep.ForceThreshold != 0 {
		t.Errorf("legacy multi-threshold fields should stay zero: soft=%d snip=%d force=%d",
			rep.SoftThreshold, rep.SnipThreshold, rep.ForceThreshold)
	}
}

// A zero window disables maintenance; the thresholds then mean nothing and must
// not be presented as if they did.
func TestContextReportLeavesThresholdsZeroWhenDisabled(t *testing.T) {
	a := New(&fakeProvider{reply: "unused"}, tool.NewRegistry(), NewSession("sys"), Options{
		ContextWindow: 0, RecentKeep: 2,
	}, event.Discard)

	rep := a.ContextReport()
	if rep.Window != 0 || rep.FoldThreshold != 0 || rep.ForceThreshold != 0 {
		t.Errorf("disabled maintenance reported thresholds: %+v", rep)
	}
}

// The breakdown classifies the same visible messages the maintenance decision
// sees: tool-role output versus chat, with the per-request schema mass counted
// separately because compaction can never reclaim it. The estimator
// (estimateMessagesTokens) prices ASCII text at one token per rune, adds 4
// framing tokens per message and 8 + id + name + arguments per tool call, so
// the fixture arithmetic is exact:
//
//	tool message   = 4 framing + 8000 content + 2 ("c1") + 4 ("dump") = 8010
//	assistant call = 4 framing + 8 call framing + 2 + 4 + 2 = 20
//	chat messages  = (4+4000) system + (4+4000) user + 20 + (4+4000) user = 12032
//
// The LocalOnly user turn must land in neither bucket. It is excluded twice —
// by the loop guard in ContextReport and again by estimateMessagesTokens —
// so deleting either guard alone stays green; removing both (or breaking the
// LocalOnly contract anywhere on the path) adds 4004 to ChatTokens and turns
// this test red.
func TestContextReportBreakdownClassifiesToolResultsAndChat(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: strings.Repeat("s", 4_000)},
		{Role: provider.RoleUser, Content: strings.Repeat("u", 4_000)},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "dump", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "dump", Content: strings.Repeat("t", 8_000)},
		{Role: provider.RoleUser, Content: strings.Repeat("v", 4_000)},
		{Role: provider.RoleUser, Content: strings.Repeat("l", 4_000), LocalOnly: true}, // local-only: neither bucket
	}
	a := agentOverForceWindow(t, prov, &Session{Messages: msgs}, 50_000)
	a.SetTools(echoRegistry()) // agentOverForceWindow starts with an empty registry; schemas must exist

	rep := a.ContextReport()
	if rep.ToolResultTokens != 8_010 {
		t.Fatalf("ToolResultTokens = %d, want 8010", rep.ToolResultTokens)
	}
	if rep.ChatTokens != 12_032 { // ChatTokens includes the system message
		t.Fatalf("ChatTokens = %d, want 12032", rep.ChatTokens)
	}
	if rep.SchemaTokens <= 0 {
		t.Fatalf("SchemaTokens = %d, want > 0 (registry has schemas)", rep.SchemaTokens)
	}
}
