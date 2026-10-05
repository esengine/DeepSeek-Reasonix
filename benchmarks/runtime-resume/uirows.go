package main

import (
	"fmt"
	"sort"
)

// The rows this arm exists for. Every other graph row compares values; these
// compare doors. A frontend that rebuilt the same graph by replaying history as
// live transitions would pass all of those and fail all of these.
const uiAuthority = "the frontend's own store, read through the real transport"

func uiRows(before, after Observation) []row {
	ui := after.UI
	if ui == nil || len(ui.Trace) == 0 {
		return []row{uiRow("the view's observation sequence", "a sequence of visible states",
			orNone(uiFailure(ui)), verdictNotMeasured)}
	}
	historical := historicalIDs(before)
	first := firstSight(ui.Trace)
	return []row{
		uiFirstPictureRow(ui.Trace),
		uiHistoryOriginRow(historical, first),
		uiGhostRow(ui.Trace, historical),
		uiInterruptionRow(ui.Trace, after),
		uiLiveRow(historical, first),
		uiRebirthRow(ui.Trace),
		uiRepublishRow(historical, ui.Deltas),
	}
}

func uiFailure(ui *UIObs) string {
	if ui == nil {
		return ""
	}
	return ui.Err
}

func uiRow(semantic, want, got, verdict string) row {
	return row{
		Semantic: semantic, Authority: uiAuthority, Artifact: "none — a sequence, not a value",
		Reconstruction: want, Before: want, After: got, Verdict: verdict,
	}
}

func held(ok bool) string {
	if ok {
		return verdictHolds
	}
	return verdictViolated
}

// uiFirstPictureRow reads the handover itself: nothing, then the authority. A
// graph that appears from a delta was assembled out of transitions, whatever it
// settles on afterwards.
func uiFirstPictureRow(trace []UIEntry) row {
	got := trace[0].Phase
	for _, e := range trace {
		if len(e.States) > 0 {
			got = trace[0].Phase + " → " + e.Origin
			break
		}
	}
	return uiRow("the first graph a resumed view draws", "loading → snapshot", got, held(got == "loading → snapshot"))
}

// uiHistoryOriginRow is the arm's whole point: an execution the dead process
// opened may enter the view only as part of an answer, never as news.
func uiHistoryOriginRow(historical map[string]bool, first map[string]string) row {
	var wrong []string
	seen := 0
	for id, origin := range first {
		if !historical[id] {
			continue
		}
		seen++
		if origin != "snapshot" {
			wrong = append(wrong, id+" via "+origin)
		}
	}
	sort.Strings(wrong)
	got := fmt.Sprintf("%d from the snapshot", seen)
	if len(wrong) > 0 {
		got = join(wrong)
	}
	return uiRow("how work from the dead process entered the view",
		fmt.Sprintf("%d executions, each from the snapshot", len(historical)), got, held(len(wrong) == 0))
}

// uiGhostRow watches every frame, not the last one: a node shown as running for
// a single render and corrected afterwards was still shown as running.
func uiGhostRow(trace []UIEntry, historical map[string]bool) row {
	ghosts := ghostSightings(trace, historical)
	return uiRow("a dead run shown as work in progress",
		"never pending, queued or running", orNone(join(ghosts)), held(len(ghosts) == 0))
}

// uiInterruptionRow compares the two lists that describe the same executions:
// what the host derived, and what the view told the user.
func uiInterruptionRow(trace []UIEntry, after Observation) row {
	want, got := wantedInterruptions(after), shownInterruptions(trace)
	return uiRow("interruptions the view names", orNone(want), orNone(got), held(want == got))
}

// uiLiveRow is the positive control. Without it a view that had stopped folding
// deltas altogether would satisfy every row above.
func uiLiveRow(historical map[string]bool, first map[string]string) row {
	var wrong, fresh []string
	for id, origin := range first {
		if historical[id] {
			continue
		}
		fresh = append(fresh, id)
		if origin != "delta" {
			wrong = append(wrong, id+" via "+origin)
		}
	}
	sort.Strings(fresh)
	sort.Strings(wrong)
	switch {
	case len(fresh) == 0:
		return uiRow("work started after the resume", "at least one, from a delta",
			"nothing new was ever drawn", verdictViolated)
	case len(wrong) > 0:
		return uiRow("work started after the resume", "each from a delta", join(wrong), verdictViolated)
	}
	return uiRow("work started after the resume", "each from a delta",
		fmt.Sprintf("%d from a delta: %s", len(fresh), join(fresh)), verdictHolds)
}

// uiRebirthRow: a node the view dropped and drew again is a second birth to
// whatever the interface hangs off an introduction, however idempotent the fold
// underneath it was.
func uiRebirthRow(trace []UIEntry) row {
	twice := rebirths(trace)
	return uiRow("a node introduced twice in one session",
		"each node introduced once", orNone(join(twice)), held(len(twice) == 0))
}

// uiRepublishRow is the rule underneath the first-appearance one, and the only
// one that catches a republication at a view that already holds the snapshot:
// after a resume, no delta may name work the dead process opened. Nothing in
// production produces one — an execution belongs to the process that opened it.
func uiRepublishRow(historical map[string]bool, deltas [][]string) row {
	named := map[string]bool{}
	for _, ids := range deltas {
		for _, id := range ids {
			if historical[id] {
				named[id] = true
			}
		}
	}
	old := sortedKeys(named)
	return uiRow("deltas naming work the dead process opened",
		"none: history is answered, never published", orNone(join(old)), held(len(old) == 0))
}

// uiArmInvalid guards the premise. The states the death has to hold are the
// whole reason this arm can say anything about ghosts, and an observer that
// never ran leaves every row below it describing nothing.
func uiArmInvalid(before, after Observation) string {
	if after.UI == nil || len(after.UI.Trace) == 0 {
		return "the frontend was never observed: " + orNone(uiFailure(after.UI))
	}
	var missing []string
	for _, state := range wantedStates(armUIGraphMixed) {
		if !holdsState(before, string(state)) {
			missing = append(missing, string(state))
		}
	}
	if len(missing) > 0 {
		return "the death did not hold " + join(missing) + ", so the view had nothing to get wrong about them"
	}
	return ""
}

func holdsState(o Observation, want string) bool {
	for _, n := range o.Graph.Nodes {
		if string(n.State) == want {
			return true
		}
	}
	return false
}
