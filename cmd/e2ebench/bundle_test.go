package main

import (
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRenderEvidenceBundlePricesFalseCompletion(t *testing.T) {
	run := func(id string, passed bool, outcome, class string, reasons ...string) result {
		r := result{task: task{ID: id}, Passed: passed}
		r.Trajectory = &trajectorySummary{BundleOutcome: outcome, DivergenceClass: class, DivergenceReasons: reasons}
		return r
	}
	got := renderEvidenceBundle([]result{
		run("a", true, "completed", "agree"),
		run("b", false, "completed", "agree"),
		run("c", false, "incomplete", "new_stricter", "claim_only", "verifier.none"),
		run("d", true, "incomplete", "new_stricter", "claim_only"),
	})
	for _, want := range []string{
		"outcomes completed ×2 · incomplete ×2",
		"**false completion** 50% (1/2 completed runs the grader failed)",
		"divergence agree ×2 · new_stricter ×2",
		"reasons claim_only ×2 · verifier.none ×1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("bundle line missing %q:\n%s", want, got)
		}
	}
	if renderEvidenceBundle([]result{{task: task{ID: "e"}}}) != "" {
		t.Fatal("runs without bundle audits must render nothing")
	}
}

func TestSummarizeTrajectoryReadsEvidenceBundle(t *testing.T) {
	path := testenv.TempDir(t) + "/bundle.trajectory.jsonl"
	lines := []string{
		`{"seq":1,"ts":1000,"evidence_bundle":{"sealed":true,"outcome":"incomplete","divergence_class":"new_stricter","divergence_reasons":["claim_only"]}}`,
		`{"seq":2,"ts":2000,"evidence_bundle":{"sealed":false,"failure_code":"trusted_state.unwritable"}}`,
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatal(err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.BundleOutcome != "incomplete" || s.DivergenceClass != "new_stricter" || !slices.Equal(s.DivergenceReasons, []string{"claim_only"}) {
		t.Fatalf("bundle = %q/%q/%v; an unsealed record must not erase the last outcome", s.BundleOutcome, s.DivergenceClass, s.DivergenceReasons)
	}
}
