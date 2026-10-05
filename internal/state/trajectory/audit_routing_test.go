package trajectory

import (
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

// The bug this guards: SubagentHandoffAudit had a producer, a struct, an
// interface and a green unit test, and still reached nothing — the type
// assertion inside event.RecordSubagentHandoff matched no sink in the chain.
// Conformance proves a method exists; only a stack proves a value arrives.
func TestSubagentHandoffSurvivesTheSessionSinkStack(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "run.trajectory.jsonl")
	r, err := New(event.Discard, path, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// boot's assembly order: Coalesce -> Sync -> CostQuote -> recorder.
	chain := event.Coalesce(event.Sync(event.NewCostQuoteSink(r, nil)), time.Millisecond)

	event.RecordSubagentHandoff(chain, event.SubagentHandoffAudit{
		Entrance: "read_only_task", Depth: 1, ReadOnly: true, Expected: true,
		Exit: "completed", Attempts: 2, Accepted: 1, Malformed: 1,
		ReportRound: 3, FinalRound: 4, ToolCallsAfterReport: 2,
		ClaimedStatus: "complete", AdjudicatedStatus: "partial",
		LoweredClaims: 1, Criteria: 3, Evidence: 4, Unresolved: 1,
	})
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := readRecords(t, path)
	if len(recs) != 1 || recs[0].SubagentHandoff == nil {
		t.Fatalf("handoff never reached the recorder: %+v", recs)
	}
	got := *recs[0].SubagentHandoff
	want := SubagentHandoff{
		Entrance: "read_only_task", Depth: 1, ReadOnly: true, Expected: true,
		Exit: "completed", Attempts: 2, Accepted: 1, Malformed: 1,
		ReportRound: 3, FinalRound: 4, ToolCallsAfterReport: 2,
		ClaimedStatus: "complete", AdjudicatedStatus: "partial",
		LoweredClaims: 1, Criteria: 3, Evidence: 4, Unresolved: 1,
	}
	if got != want {
		t.Fatalf("handoff record = %+v, want %+v", got, want)
	}
}
