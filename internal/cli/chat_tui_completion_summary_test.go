package cli

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestCompletionSummaryOutputIsTiered(t *testing.T) {
	complete := &event.CompletionSummaryInfo{
		Preset: "balanced", Verdict: "complete", Mutations: 2,
		ChecksPassed: 4, Review: "passed",
	}
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.CompletionSummary, Completion: complete})
	if len(*m.pendingCommit) != 0 {
		t.Fatalf("ordinary completion summary should be silent, committed=%v", *m.pendingCommit)
	}

	partial := &event.CompletionSummaryInfo{
		Preset: "balanced", Verdict: "partial", Mutations: 2,
		ChecksPassed: 3, ChecksFailed: 1, Review: "passed", GapKinds: []string{"stale_check"},
	}
	m = newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.CompletionSummary, Completion: partial})
	lines := strings.Join(*m.pendingCommit, "\n")
	if !strings.Contains(lines, "!") || strings.Contains(lines, "balanced") || strings.Contains(lines, "stale_check") {
		t.Fatalf("non-verbose partial summary should be a localized short warning, committed=%q", lines)
	}

	m = newTestChatTUI()
	m.showReasoning = true
	m.ingestEvent(event.Event{Kind: event.CompletionSummary, Completion: partial})
	lines = strings.Join(*m.pendingCommit, "\n")
	if !strings.Contains(lines, "stale_check") || !strings.Contains(lines, "partial") || strings.Contains(lines, "balanced") {
		t.Fatalf("verbose mode should include raw completion details without a mode label, committed=%q", lines)
	}

	unreviewed := &event.CompletionSummaryInfo{
		Verdict: "partial", Mutations: 1, Review: "unavailable", GapKinds: []string{"unreviewed_change"},
	}
	m = newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.CompletionSummary, Completion: unreviewed})
	if len(*m.pendingCommit) != 0 {
		t.Fatalf("standard unreviewed changes must stay silent, committed=%v", *m.pendingCommit)
	}
}

func TestCompletionSummaryUsesTurnTimeAttention(t *testing.T) {
	requiredSuppressed := &event.CompletionSummaryInfo{
		Verdict: "partial", ChecksSuppressed: 1, GapKinds: []string{"suppressed_requirement"},
		Floor: "delivery", Attention: true,
	}
	if !completionSummaryNeedsAttention(requiredSuppressed, "standard") {
		t.Fatal("backend attention must survive a later floor change")
	}
	quietStandard := &event.CompletionSummaryInfo{
		Verdict: "partial", GapKinds: []string{"unverified_change"},
		Floor: "standard", Attention: false,
	}
	if completionSummaryNeedsAttention(quietStandard, "delivery") {
		t.Fatal("historical standard summary must not be reclassified by the current floor")
	}
	legacySuppressed := &event.CompletionSummaryInfo{Verdict: "partial", ChecksSuppressed: 1}
	if !completionSummaryNeedsAttention(legacySuppressed, "delivery") {
		t.Fatal("legacy suppressed checks must fail closed")
	}
}
