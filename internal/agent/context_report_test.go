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

	a.lastUsage.Store(&provider.Usage{
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
