package main

import (
	"fmt"
	"strings"
)

const timelineWidth = 48

// renderTimelines draws each checkpointed run's lifecycle as the argument-
// ending picture: start → first useful mutation → CORRECT → final, with the
// before/after-correct tallies underneath.
func renderTimelines(results []result) string {
	var b strings.Builder
	for _, r := range results {
		if len(r.Checkpoints) == 0 || r.WallMs == 0 {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("### Timelines\n\n")
		}
		fmt.Fprintf(&b, "`%s`\n\n```\n%s```\n\n", r.ID, taskTimeline(r))
	}
	return b.String()
}

func taskTimeline(r result) string {
	pos := func(ms int64) int {
		p := int(ms * timelineWidth / r.WallMs)
		return min(max(p, 0), timelineWidth)
	}
	markers := []struct {
		at    int
		label string
	}{{0, "start"}}
	if r.FirstUsefulMs > 0 {
		markers = append(markers, struct {
			at    int
			label string
		}{pos(r.FirstUsefulMs), "useful mutation " + dur(r.FirstUsefulMs)})
	}
	if r.FirstCorrectMs > 0 {
		markers = append(markers, struct {
			at    int
			label string
		}{pos(r.FirstCorrectMs), "CORRECT " + dur(r.FirstCorrectMs)})
	}
	markers = append(markers, struct {
		at    int
		label string
	}{timelineWidth, "final " + dur(r.WallMs)})

	bar := []rune(strings.Repeat("─", timelineWidth+1))
	for _, m := range markers {
		bar[m.at] = '│'
	}
	var out strings.Builder
	out.WriteString(string(bar) + "\n")
	for _, m := range markers {
		out.WriteString(strings.Repeat(" ", m.at) + "^ " + m.label + "\n")
	}
	if r.Passed && r.PostSolveWasteMs > 0 {
		fmt.Fprintf(&out, "post-solve waste %s (%s of wall)\n", dur(r.PostSolveWasteMs), pct(int(r.PostSolveWasteMs), int(r.WallMs)))
	}
	if r.FirstCorrectMs > 0 {
		fmt.Fprintf(&out, "rounds %d→%d · calls %d→%d · after correct: verifications %d · reviews %d · mutations %d\n",
			r.RoundsBeforeCorrect, r.RoundsAfterCorrect,
			r.CallsBeforeCorrect, r.CallsAfterCorrect,
			r.VerifyAfterCorrect, r.ReviewsAfterCorrect, r.MutationsAfterCorrect)
	}
	return out.String()
}

// renderDiagnosis applies the decide-then-optimize tree to the measured
// signals and names the knife, in priority order: capability first (you
// cannot stop what never solves), damage second (safety bounds any stop
// policy), then the bigger of the termination tail and the exploration road.
func renderDiagnosis(results []result) string {
	var wall, waste, ttfum int64
	checkpointed, neverCorrect, damaged, withCorrect := 0, 0, 0, 0
	for _, r := range results {
		if len(r.Checkpoints) == 0 {
			continue
		}
		checkpointed++
		wall += r.WallMs
		if r.FirstCorrectMs == 0 && !r.Passed {
			neverCorrect++
			continue
		}
		withCorrect++
		if r.RegressedAfterCorrect {
			damaged++
		}
		waste += r.PostSolveWasteMs
		ttfum += r.FirstUsefulMs
	}
	if checkpointed == 0 || wall == 0 {
		return ""
	}
	verdict := ""
	switch {
	case neverCorrect*100 >= checkpointed*30:
		verdict = fmt.Sprintf("**never-correct dominates** (%s of runs) → reasoning quality: better verifier, subagent specialization, or a better model — latency work is premature", pct(neverCorrect, checkpointed))
	case withCorrect > 0 && damaged*100 >= withCorrect*10:
		verdict = fmt.Sprintf("**correct→incorrect regressions** (%s) → conservative stop policy plus a mutation-after-pass guard before anything else", pct(damaged, withCorrect))
	case waste >= ttfum:
		verdict = fmt.Sprintf("**post-solve waste dominates** (%s of wall vs TTFUM %s) → TaskContract + evidence graph + early termination", pct(int(waste), int(wall)), pct(int(ttfum), int(wall)))
	default:
		verdict = fmt.Sprintf("**exploration dominates** (TTFUM %s of wall vs waste %s) → fault localization, context retrieval, planner and tool choice", pct(int(ttfum), int(wall)), pct(int(waste), int(wall)))
	}
	return "**Diagnosis**: " + verdict + "\n\n"
}
