package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// renderEvidenceBundle prices the host-evidence outcome the way the completion
// report is priced: a completed run the grader failed is a false completion.
// The divergence census is the stage-1 product — it says which old path an
// accepted contract has to remove.
func renderEvidenceBundle(results []result) string {
	outcomes := map[string]int{}
	classes := map[string]int{}
	reasons := map[string]int{}
	recorded, completed, falseCompleted := 0, 0, 0
	for _, r := range results {
		t := r.Trajectory
		if t == nil || t.BundleOutcome == "" {
			continue
		}
		recorded++
		outcomes[t.BundleOutcome]++
		classes[t.DivergenceClass]++
		for _, reason := range t.DivergenceReasons {
			reasons[reason]++
		}
		if t.BundleOutcome == "completed" {
			completed++
			if !r.Passed {
				falseCompleted++
			}
		}
	}
	if recorded == 0 {
		return ""
	}
	line := fmt.Sprintf("**Evidence bundle**: outcomes %s · **false completion** %s (%d/%d completed runs the grader failed) · divergence %s",
		census(outcomes), pct(falseCompleted, completed), falseCompleted, completed, census(classes))
	if len(reasons) > 0 {
		line += " · reasons " + census(reasons)
	}
	return line + "\n\n"
}

// census renders counts in descending order, ties by name, so two reports of
// the same runs read the same.
func census(counts map[string]int) string {
	keys := slices.Collect(maps.Keys(counts))
	slices.SortFunc(keys, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, counts[k]))
	}
	return strings.Join(parts, " · ")
}
