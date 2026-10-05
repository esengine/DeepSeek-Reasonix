package cli

import (
	"testing"

	"reasonix/internal/contract/event"
)

// The compliance denominator is the point of this test: a child the provider
// killed never reached the point of closing, so it must leave the denominator
// rather than count as a refusal. Routing is asserted too — the audit is sent
// through a wrapper, not called on the sink directly, because a method that
// exists and is never reached is the bug this channel already had once.
func TestMetricsSinkCountsSubagentHandoffCompliance(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	chain := event.Sync(s)

	event.RecordSubagentHandoff(chain, event.SubagentHandoffAudit{
		Expected: true, Exit: "completed", ReadOnly: true,
	})
	event.RecordSubagentHandoff(chain, event.SubagentHandoffAudit{
		Expected: true, Exit: "cancelled",
	})
	event.RecordSubagentHandoff(chain, event.SubagentHandoffAudit{
		Expected: true, Exit: "completed",
		Attempts: 1, Accepted: 1, ReportRound: 2, FinalRound: 2, LoweredClaims: 3,
	})

	m := s.m
	if m.SubagentHandoffs != 3 || m.SubagentHandoffExpected != 3 || m.SubagentHandoffJudged != 2 {
		t.Fatalf("totals = %d/%d/%d, want 3 observed, 3 expected, 2 judged",
			m.SubagentHandoffs, m.SubagentHandoffExpected, m.SubagentHandoffJudged)
	}
	if m.SubagentHandoffNotTried != 1 || m.SubagentHandoffAttempted != 1 || m.SubagentHandoffAccepted != 1 {
		t.Fatalf("never-attempted/attempted/accepted = %d/%d/%d, want 1/1/1 — the cancelled run must not be either",
			m.SubagentHandoffNotTried, m.SubagentHandoffAttempted, m.SubagentHandoffAccepted)
	}
	if m.SubagentHandoffClosedWith != 1 || m.SubagentHandoffLowered != 3 || m.SubagentHandoffReadOnly != 1 {
		t.Fatalf("closed/lowered/read-only = %d/%d/%d, want 1/3/1",
			m.SubagentHandoffClosedWith, m.SubagentHandoffLowered, m.SubagentHandoffReadOnly)
	}
	if m.SubagentHandoffExits["completed"] != 2 || m.SubagentHandoffExits["cancelled"] != 1 {
		t.Fatalf("exits = %v, want 2 completed and 1 cancelled", m.SubagentHandoffExits)
	}
}
