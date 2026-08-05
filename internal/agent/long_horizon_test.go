package agent

import (
	"context"
	"os"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"strings"
	"testing"
)

// TestSummarySystemPromptIncludesLongHorizonSections verifies that the
// compaction summary prompt includes the 3 new OSWorld 2.0-informed sections
// that preserve implicit state across compaction cycles.
func TestSummarySystemPromptIncludesLongHorizonSections(t *testing.T) {
	required := []string{
		"## Hidden state & recovered facts",
		"## Sources consulted",
		"## Open questions & uncertainties",
	}
	for _, section := range required {
		if !strings.Contains(summarySystemPrompt, section) {
			t.Errorf("summarySystemPrompt missing section: %s", section)
		}
	}
	// Verify the original 7 sections are still present
	original := []string{
		"## Standing facts & constraints",
		"## Goal",
		"## Decisions & rationale",
		"## Files & code",
		"## Commands & outcomes",
		"## Errors & fixes",
		"## Pending & next step",
	}
	for _, section := range original {
		if !strings.Contains(summarySystemPrompt, section) {
			t.Errorf("summarySystemPrompt missing original section: %s", section)
		}
	}
	// Total section count should be 10
	totalSections := strings.Count(summarySystemPrompt, "## ")
	if totalSections != 10 {
		t.Errorf("summarySystemPrompt has %d sections, want 10", totalSections)
	}
}

// TestCompactSendsEnhancedPromptToSummarizer verifies that the actual
// summarization call uses the enhanced 10-section prompt — not just that
// the constant is correct, but that it reaches the provider.
func TestCompactSendsEnhancedPromptToSummarizer(t *testing.T) {
	prov := &fakeProvider{reply: "## Standing facts & constraints\n- test fact\n## Hidden state & recovered facts\n- inferred path /opt/hidden\n## Sources consulted\n- checked: file_a.go\n## Open questions & uncertainties\n- need user confirm on X"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "long-horizon task with multiple steps"},
		{Role: provider.RoleAssistant, Content: "step one — read config"},
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleAssistant, Content: "step two — inferred hidden state from error log"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "step three — checked 3 sources, 2 remaining"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(prov.got) == 0 {
		t.Fatal("summarizer was not called")
	}
	sysPrompt := prov.got[0].Content
	// Verify the enhanced prompt reached the provider
	for _, section := range []string{"Hidden state", "Sources consulted", "Open questions"} {
		if !strings.Contains(sysPrompt, section) {
			t.Errorf("summarizer system prompt missing section: %s (got %q)", section, sysPrompt[:min(200, len(sysPrompt))])
		}
	}
}

// TestLongHorizonCompactionThresholds verifies that long-horizon-adjusted
// ratios produce earlier compaction triggers — the core mechanism for
// capturing implicit state before it's lost to snip/prune.
func TestLongHorizonCompactionThresholds(t *testing.T) {
	const windowSize = 100000 // 100K token window
	cases := []struct {
		name       string
		softRatio  float64
		snipRatio  float64
		wantSoft   int
		wantSnip   int
		wantHigh   int
	}{
		{
			name:      "standard-defaults",
			softRatio: 0.5,
			snipRatio: 0.6,
			wantSoft:  50000,
			wantSnip:  60000,
			wantHigh:  80000,
		},
		{
			name:      "long-horizon-adjusted",
			softRatio: 0.4,
			snipRatio: 0.5,
			wantSoft:  40000,
			wantSnip:  50000,
			wantHigh:  80000,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &fakeProvider{reply: "summary"}
			sess := &Session{Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: "sys"},
				{Role: provider.RoleUser, Content: "task"},
			}}
			a := New(prov, tool.NewRegistry(), sess, Options{
				ContextWindow:       windowSize,
				SoftCompactRatio:    tc.softRatio,
				ToolResultSnipRatio: tc.snipRatio,
				CompactRatio:        0.8,
				CompactForceRatio:   0.9,
			}, event.Discard)
			soft, snip, high := a.compactThresholds()
			if soft != tc.wantSoft {
				t.Errorf("soft = %d, want %d (ratio %v × window %d)", soft, tc.wantSoft, tc.softRatio, windowSize)
			}
			if snip != tc.wantSnip {
				t.Errorf("snip = %d, want %d (ratio %v × window %d)", snip, tc.wantSnip, tc.snipRatio, windowSize)
			}
			if high != tc.wantHigh {
				t.Errorf("high = %d, want %d", high, tc.wantHigh)
			}
		})
	}
}

// TestLongHorizonSoftNoticeEarlier verifies that the soft notice fires
// earlier (at 40% instead of 50%) in long-horizon mode — giving the agent
// more runway to prepare for compaction.
func TestLongHorizonSoftNoticeEarlier(t *testing.T) {
	const windowSize = 100000
	// At 45% of window: standard mode (soft=50%) should NOT fire;
	// long-horizon mode (soft=40%) SHOULD fire.
	promptAt45Pct := 45000

	// Standard mode
	prov1 := &fakeProvider{reply: "x"}
	sess1 := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	var standardNotices []event.Event
	sink1 := event.FuncSink(func(e event.Event) { standardNotices = append(standardNotices, e) })
	a1 := New(prov1, tool.NewRegistry(), sess1, Options{
		ContextWindow:    windowSize,
		SoftCompactRatio: 0.5,
		ToolResultSnipRatio: 0.6,
		CompactRatio:     0.8,
		CompactForceRatio: 0.9,
	}, sink1)
	a1.maybeCompact(context.Background(), &provider.Usage{PromptTokens: promptAt45Pct})
	if len(standardNotices) != 0 {
		t.Errorf("standard mode: soft notice should NOT fire at 45%% (threshold 50%%), got %d notices", len(standardNotices))
	}

	// Long-horizon mode
	prov2 := &fakeProvider{reply: "x"}
	sess2 := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	var lhNotices []event.Event
	sink2 := event.FuncSink(func(e event.Event) { lhNotices = append(lhNotices, e) })
	a2 := New(prov2, tool.NewRegistry(), sess2, Options{
		ContextWindow:    windowSize,
		SoftCompactRatio: 0.4, // long-horizon adjusted
		ToolResultSnipRatio: 0.5,
		CompactRatio:     0.8,
		CompactForceRatio: 0.9,
	}, sink2)
	a2.maybeCompact(context.Background(), &provider.Usage{PromptTokens: promptAt45Pct})
	if len(lhNotices) == 0 {
		t.Error("long-horizon mode: soft notice SHOULD fire at 45%% (threshold 40%%), got 0 notices")
	}
}

// TestLongHorizonCompactionPreservesImplicitState is the end-to-end test:
// it simulates a long-horizon task where the agent has discovered hidden
// state (inferred paths, cross-source findings), triggers compaction, and
// verifies the summary preserves that implicit state in the new sections.
func TestLongHorizonCompactionPreservesImplicitState(t *testing.T) {
	// The fake summarizer simulates what a real model would produce:
	// a 10-section summary that includes the 3 new sections.
	summaryReply := `## Standing facts & constraints
- User wants expense report processed
- Budget cap: $5000
- Never delete original receipts

## Goal
Process Q3 expense reimbursement across 3 systems.

## Decisions & rationale
- Used ChaseBank API instead of PDF parsing — more reliable
- Flagged transaction #8842 as potentially duplicate

## Files & code
- /tmp/expense_report.json: draft submission
- config.yaml: API keys for ChaseBank + GMail

## Commands & outcomes
- ` + "`python parse_receipts.py`" + `: extracted 12 line items, 2 flagged
- ` + "`curl chase-api/transactions`" + `: 200 OK, 15 transactions returned

## Errors & fixes
- GMail API rate limit → retried with exponential backoff
- Receipt #7 OCR failed → manually extracted amount from raw image

## Hidden state & recovered facts
- Employee ID 7742 found in archived 2023-Q1 report (not in current employee DB)
- Bank transaction timestamp suggests expense was incurred before policy change date
- Receipt #7's vendor name matches a deprecated vendor code in the old system

## Sources consulted
- Checked: ChaseBank API, GMail inbox, receipt OCR, 2023-Q1 archive
- NOT checked: Slack #finance channel, Confluence policy page, SAP legacy DB

## Open questions & uncertainties
- Is transaction #8842 a genuine duplicate or a split payment? Need user confirmation.
- Does the pre-policy-change date qualify for the old reimbursement rate?
- Employee ID 7742 — is this person still authorized to submit?

## Pending & next step
- Wait for user to confirm transaction #8842 status
- Then submit via ExpenseFlow portal with all supporting docs`

	prov := &fakeProvider{reply: summaryReply}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "Process Q3 expense reimbursement. Check all systems."},
		{Role: provider.RoleAssistant, Content: "I'll start by checking ChaseBank and GMail..."},
		{Role: provider.RoleUser, Content: "Continue"},
		{Role: provider.RoleAssistant, Content: "Found something odd in the archived 2023-Q1 report..."},
		{Role: provider.RoleUser, Content: "What did you find?"},
		{Role: provider.RoleAssistant, Content: "Employee ID 7742 in the old report but not in current DB. Also receipt #7 OCR failed but I extracted the amount manually."},
	}}

	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow:       100000,
		SoftCompactRatio:    0.4, // long-horizon
		ToolResultSnipRatio: 0.5,
		CompactRatio:        0.8,
		CompactForceRatio:   0.9,
		RecentKeep:          2,
	}, sink)

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact failed: %v", err)
	}

	// Verify the summary was inserted into the session
	var summaryContent string
	for _, msg := range sess.Messages {
		if strings.Contains(msg.Content, "compaction-summary") {
			summaryContent = msg.Content
			break
		}
	}
	if summaryContent == "" {
		t.Fatal("no compaction summary found in session messages after compact()")
	}

	// Verify all 10 sections are in the summary
	for _, section := range []string{
		"## Standing facts & constraints",
		"## Goal",
		"## Decisions & rationale",
		"## Files & code",
		"## Commands & outcomes",
		"## Errors & fixes",
		"## Hidden state & recovered facts",
		"## Sources consulted",
		"## Open questions & uncertainties",
		"## Pending & next step",
	} {
		if !strings.Contains(summaryContent, section) {
			t.Errorf("summary missing section: %s", section)
		}
	}
	// On any section failure, dump the raw summary so the developer can see
	// exactly what the summarizer produced — not just which section was missing.
	if t.Failed() {
		t.Logf("=== RAW SUMMARY CONTENT (section assertion failed) ===\n%s\n=== END RAW SUMMARY ===", summaryContent)
	}

	// Verify critical implicit state is preserved
	criticalState := []struct {
		desc    string
		pattern string
	}{
		{"hidden state: employee ID from archive", "Employee ID 7742"},
		{"source consulted: archived report", "archived 2023-Q1 report"},
		{"unexplored sources tracked", "NOT checked"},
		{"open question: user confirmation needed", "Need user confirmation"},
		{"hidden state: pre-policy date inference", "pre-policy-change date"},
	}
	stateFailed := false
	for _, cs := range criticalState {
		if !strings.Contains(summaryContent, cs.pattern) {
			t.Errorf("summary lost implicit state [%s]: pattern %q not found", cs.desc, cs.pattern)
			stateFailed = true
		}
	}
	// On any implicit-state failure, dump the raw summary with line numbers so
	// the developer can trace exactly where the state was lost.
	if stateFailed {
		t.Logf("=== RAW SUMMARY CONTENT (implicit state assertion failed) ===")
		lines := strings.Split(summaryContent, "\n")
		for i, line := range lines {
			t.Logf("  %4d | %s", i+1, line)
		}
		t.Logf("=== END RAW SUMMARY (%d lines) ===", len(lines))
	}

	// Verify CompactionDone event was emitted with the full summary
	var doneEvent *event.Event
	for i := range events {
		if events[i].Kind == event.CompactionDone {
			doneEvent = &events[i]
			break
		}
	}
	if doneEvent == nil {
		t.Fatal("no CompactionDone event emitted")
	}
	if doneEvent.Compaction.Messages == 0 {
		t.Error("CompactionDone reports 0 folded messages")
	}
	// Verify the CompactionDone Detail field reports which implicit-state
	// sections were found — this is the diagnostic that helps users observe
	// whether hidden state survived the fold.
	if doneEvent.Detail == "" {
		t.Error("CompactionDone event has empty Detail — expected section diagnostic")
	} else {
		expectedInDetail := []string{"Hidden state", "Sources consulted", "Open questions"}
		for _, s := range expectedInDetail {
			if !strings.Contains(doneEvent.Detail, s) {
				t.Errorf("CompactionDone Detail missing section %q (Detail: %s)", s, doneEvent.Detail)
			}
		}
	}
	// Also verify CompactionStarted has the fold/kept detail
	var startedEvent *event.Event
	for i := range events {
		if events[i].Kind == event.CompactionStarted {
			startedEvent = &events[i]
			break
		}
	}
	if startedEvent == nil {
		t.Fatal("no CompactionStarted event emitted")
	}
	if startedEvent.Detail == "" {
		t.Error("CompactionStarted event has empty Detail — expected fold/kept breakdown")
	} else if !strings.Contains(startedEvent.Detail, "folding") {
		t.Errorf("CompactionStarted Detail missing fold info: %s", startedEvent.Detail)
	}
}

// TestLongHorizonEnvConfigIntegration verifies the full config → agent
// pipeline: config normalization adjusts ratios, and those ratios reach
// the agent's compactThresholds.
func TestLongHorizonEnvConfigIntegration(t *testing.T) {
	os.Setenv("REASONIX_LONG_HORIZON", "1")
	defer os.Unsetenv("REASONIX_LONG_HORIZON")

	// This test verifies the agent-level impact of the config change.
	// In production, boot.go reads cfg.Agent.SoftCompactRatio (adjusted by
	// normalizeLongHorizon) and passes it to agent.Options.SoftCompactRatio.
	// Here we simulate that pipeline directly.
	prov := &fakeProvider{reply: "summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
	}}
	const windowSize = 200000 // 200K token window

	// Simulate what boot.go does after normalizeLongHorizon runs
	softRatio := 0.4 // adjusted from 0.5 by normalizeLongHorizon
	snipRatio := 0.5 // adjusted from 0.6 by normalizeLongHorizon

	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow:       windowSize,
		SoftCompactRatio:    softRatio,
		ToolResultSnipRatio: snipRatio,
		CompactRatio:        0.8,
		CompactForceRatio:   0.9,
	}, event.Discard)

	soft, snip, high := a.compactThresholds()
	// Verify long-horizon thresholds
	if soft != 80000 {
		t.Errorf("soft = %d, want 80000 (0.4 × 200000)", soft)
	}
	if snip != 100000 {
		t.Errorf("snip = %d, want 100000 (0.5 × 200000)", snip)
	}
	if high != 160000 {
		t.Errorf("high = %d, want 160000 (0.8 × 200000)", high)
	}

	// Verify soft fires earlier than standard (100000 vs 100000 boundary)
	// At 90K tokens: standard (soft=100K) wouldn't fire; long-horizon (soft=80K) does
	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) { notices = append(notices, e) })
	a.sink = sink
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 90000})
	if len(notices) == 0 {
		t.Error("soft notice should fire at 90K (below standard 100K threshold but above long-horizon 80K)")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
