package memorycompiler

import (
	"context"
	"testing"
)

// TestMemoryV5SurvivesLongRun is a regression guard for two bugs where the
// production-hardening layer disabled the compiler and corrupted the trace log
// over a normal multi-turn session:
//
//   - Bug 1: StartTurn stopped injecting the contract once learned memory nodes
//     reached the GC cap (which equalled the hardening memory-node budget), and
//     never recovered.
//   - Bug 2: a genuinely successful turn with more tool calls than the budget was
//     rewritten to partial_success in the persisted trace.
//
// The original author tests only drove 1-2 turns and missed both. This drives 80
// realistic turns (25 successful tool calls each, above the old 20 budget) and
// asserts the compiler stays alive and the trace log reflects real outcomes.
func TestMemoryV5SurvivesLongRun(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	if rt == nil {
		t.Fatal("nil runtime")
	}
	ctx := context.Background()
	const goal = "refactor the auth module and make the failing tests pass"

	const turns = 80
	contractsAfterWarmup := 0
	for i := 0; i < turns; i++ {
		compiled, turn := rt.StartTurn(ctx, goal, nil)
		if turn == nil {
			t.Fatalf("turn %d: nil turn", i)
		}
		recs := make([]ToolRecord, 25) // 25 > the old 20 tool-call budget
		for j := range recs {
			recs[j] = ToolRecord{Name: "read_file", ReadOnly: true, Output: "ok"}
		}
		turn.RecordToolResults(recs)
		turn.Finish(nil) // err == nil => genuine success
		if i >= 20 && compiled != "" {
			contractsAfterWarmup++
		}
	}

	// Bug 1 regression: after warmup (memory has accumulated past the GC cap) the
	// compiler must keep producing contracts, not fall silent forever.
	if contractsAfterWarmup == 0 {
		t.Fatal("Bug 1 regression: compiler produced no contract in turns 20..80; it went silent once memory filled up")
	}

	// Bug 2 regression: every genuinely successful turn must be recorded as
	// success in the trace log, never demoted by the hardening verdict.
	traces := readTraces(t, dir)
	demoted := 0
	for _, tr := range traces {
		if tr.Outcome != "success" {
			demoted++
		}
	}
	if demoted > 0 {
		t.Fatalf("Bug 2 regression: %d/%d genuinely successful turns were demoted to non-success in the trace log", demoted, len(traces))
	}

	// The strategy learning signal must reflect real success, never fabricated
	// failures derived from a demoted outcome.
	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.Failures > 0 {
			t.Errorf("strategy %q recorded %d failures from genuinely successful turns", s.ID, s.Failures)
		}
	}
}
