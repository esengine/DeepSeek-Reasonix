package main

import "testing"

// The cost arm is deterministic, so it doubles as the regression guard for a
// growing session whose complete summary prefix still fits the model window.
func TestRepeatedFoldsStayBoundedAndKeepSucceeding(t *testing.T) {
	const (
		window   = 1_000_000
		gens     = 6
		reserve  = 8192 // summaryOutputReserve: the digest the call must still return
		maxCalls = 1    // Harness-style folding issues one complete-prefix request
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

// Harness-style compaction never privately slices an oversized prefix or
// fabricates a digest. Admission failure is explicit and installs no summary.
func TestOversizedSummaryPrefixFailsWithoutPrivateShortening(t *testing.T) {
	const window = 64_000
	res, err := runCost(1, window, arms{})
	if err != nil {
		t.Fatalf("runCost: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("generations = %d, want 1", len(res))
	}
	r := res[0]
	if r.Error == "" {
		t.Fatal("oversized complete prefix unexpectedly succeeded")
	}
	if r.SummarizerCalls != 1 {
		t.Fatalf("summarizer calls = %d, want one failed request", r.SummarizerCalls)
	}
	if r.LargestCall <= window {
		t.Fatalf("largest call = %d, want an input over window %d", r.LargestCall, window)
	}
	if r.ProjectionTokens != 0 {
		t.Fatalf("failed oversized fold installed a %d-token projection", r.ProjectionTokens)
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
