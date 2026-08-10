package main

import "testing"

// The cost arm is deterministic, so it doubles as the regression guard for the
// property it was written to measure: a session that keeps growing must keep
// folding, and no single summarizer call may outgrow the window.
func TestRepeatedFoldsStayBoundedAndKeepSucceeding(t *testing.T) {
	const (
		window   = 64_000
		gens     = 6
		reserve  = 8192 // summaryOutputReserve: the digest the call must still return
		maxCalls = 7    // maxSummarySpans + the merge pass
	)
	res, err := runCost(gens, window, arms{})
	if err != nil {
		t.Fatalf("runCost: %v", err)
	}
	if len(res) != gens {
		t.Fatalf("generations = %d, want %d", len(res), gens)
	}
	for _, r := range res {
		if r.Error != "" {
			t.Errorf("gen %d failed to fold: %s", r.Gen+1, r.Error)
		}
		if r.LargestCall+reserve > window {
			t.Errorf("gen %d largest summarizer call = %d tokens est.; with %d reserved for the digest that overflows the %d window", r.Gen+1, r.LargestCall, reserve, window)
		}
		if r.SummarizerCalls > maxCalls {
			t.Errorf("gen %d cost %d summarizer calls, over the %d ceiling", r.Gen+1, r.SummarizerCalls, maxCalls)
		}
		if r.ProjectionTokens == 0 || r.ProjectionTokens >= r.CanonicalTokens {
			t.Errorf("gen %d projection = %d tokens vs canonical %d; the fold saved nothing", r.Gen+1, r.ProjectionTokens, r.CanonicalTokens)
		}
	}
	if last := res[len(res)-1]; last.CanonicalTokens <= res[0].CanonicalTokens {
		t.Fatalf("canonical did not grow across generations: %d -> %d", res[0].CanonicalTokens, last.CanonicalTokens)
	}
}

// A probe must score the stale answer as lost even when the model hedges its
// way to mentioning the right one — "yes, but it has not been re-run since"
// is the exact shape a drifting digest produces.
func TestProbeScoringRejectsHedges(t *testing.T) {
	var freshness probe
	for _, p := range probeSuite() {
		if p.class == "verification-freshness" {
			freshness = p
		}
	}
	if freshness.class == "" {
		t.Fatal("verification-freshness probe missing from the suite")
	}
	for _, tc := range []struct {
		answer string
		want   bool
	}{
		{"No.", true},
		{"no — config/format.go changed after the last run", true},
		{"Yes", false},
		{"Yes, but it has not been re-run since the edit", false},
		{"I am not sure", false},
	} {
		if got := freshness.score(tc.answer); got != tc.want {
			t.Errorf("score(%q) = %v, want %v", tc.answer, got, tc.want)
		}
	}
}
