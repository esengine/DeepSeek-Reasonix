package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A fold may only remove a run of markers from the middle: the pinned head
// stays, the retained tail stays, and everything between them becomes the
// digest. Any other shape means the view lost something no fold folded.
const (
	verdictContiguous = "contiguous"
	verdictHole       = "HOLE"
	markPrefix        = "PROBE-MARK-"
	echoPrefix        = "PROBE-ECHO-"
)

// refoldRows report what the second fold did. They are their own set because
// the comparison is inside one process — the view before the refold against
// the view after it — not across the process boundary.
func refoldRows(prefold, postfold, resumed Observation, total int) []row {
	return []row{
		coverageRow("fold 1: stored body and coverage", prefold),
		coverageRow("fold 2: stored body and coverage", postfold),
		continuityRow("assistant work visible after fold 1", prefold, total),
		continuityRow("assistant work visible after fold 2", postfold, total),
		continuityRow("assistant work visible after restart", resumed, total),
		turnsRow("user turns visible after fold 2", postfold),
	}
}

func coverageRow(semantic string, o Observation) row {
	return row{
		Semantic: semantic, Authority: "compaction sidecar", Artifact: "<stem>.context.json",
		Reconstruction: "projection.Messages + canonical[CoveredCount:]",
		Before:         fmt.Sprintf("canonical=%d", o.Transcript.Messages),
		After: fmt.Sprintf("body=%d covered=%d spliced=+%d view=%d",
			o.Sidecar.Messages, o.Sidecar.CoveredCount, o.View.SplicedFromTail, o.View.Messages),
		Verdict: verdictStable,
	}
}

// continuityRow judges the assistant side: a fold replaces one run of replies
// with a digest, so anything but a single surviving run is a loss no fold made.
func continuityRow(semantic string, o Observation, total int) row {
	shape := markerShape(o.View.Echoes, echoPrefix)
	verdict := verdictContiguous
	if reason := markerHole(o.View.Echoes, echoPrefix, total); reason != "" {
		verdict, shape = verdictHole, shape+" — "+reason
	}
	return row{
		Semantic: semantic, Authority: "the spliced model-visible view",
		Artifact: "none: derived", Reconstruction: "assistant replies surviving in the view",
		Before: fmt.Sprintf("emitted 1-%d", total), After: shape, Verdict: verdict,
	}
}

// turnsRow reports the user side beside it: the retention budget keeps those
// verbatim, which is why a one-sided tag could not see a dropped reply.
func turnsRow(semantic string, o Observation) row {
	return row{
		Semantic: semantic, Authority: "the spliced model-visible view",
		Artifact: "none: derived", Reconstruction: "user turns surviving in the view",
		Before: "kept by the retention budget",
		After:  markerShape(o.View.Markers, markPrefix), Verdict: verdictStable,
	}
}

// pad renders a tag number the way the scripted turns emit it.
func pad(n int) string { return fmt.Sprintf("%03d", n) }

func markerNumbers(markers []string, prefix string) []int {
	seen := map[int]bool{}
	var out []int
	for _, m := range markers {
		n, err := strconv.Atoi(strings.TrimPrefix(m, prefix))
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// markerShape renders the surviving markers as runs, which is the form the
// judgement is about: "1, 9-12" says a fold took 2-8.
func markerShape(markers []string, prefix string) string {
	nums := markerNumbers(markers, prefix)
	if len(nums) == 0 {
		return "none"
	}
	var runs []string
	start := nums[0]
	for i := 1; i <= len(nums); i++ {
		if i < len(nums) && nums[i] == nums[i-1]+1 {
			continue
		}
		if end := nums[i-1]; end == start {
			runs = append(runs, strconv.Itoa(start))
		} else {
			runs = append(runs, fmt.Sprintf("%d-%d", start, end))
		}
		if i < len(nums) {
			start = nums[i]
		}
	}
	return strings.Join(runs, ", ")
}

// markerHole names the violation, or returns "" when the shape is one a fold
// can produce: an optional pinned head starting at 1, then one run reaching the
// last turn. Two gaps, or a tail that stops short of it, are losses.
func markerHole(markers []string, prefix string, total int) string {
	nums := markerNumbers(markers, prefix)
	if len(nums) == 0 {
		return "the view shows no turn at all"
	}
	runs := strings.Split(markerShape(markers, prefix), ", ")
	if last := nums[len(nums)-1]; last != total {
		return fmt.Sprintf("the newest turn %d is missing", total)
	}
	if len(runs) > 2 {
		return fmt.Sprintf("%d separate runs; a fold removes one", len(runs))
	}
	if len(runs) == 2 && nums[0] != 1 {
		return fmt.Sprintf("the kept head starts at %d, not the pinned first turn", nums[0])
	}
	return ""
}
