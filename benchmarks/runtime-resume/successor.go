package main

import "fmt"

// successorRows ask what happens to a recorded interruption once work carries
// on. The derivation has no way out of "interrupted" today, so these rows are
// there to say what that costs rather than to grade it.
func successorRows(arm string, successor, final Observation) []row {
	return []row{{
		Semantic: "the interruption after work continued", Authority: "the adjudication log",
		Artifact: "<stem>.adjudication.jsonl", Reconstruction: "still derived from an open record with no owner",
		Before: interruptionState(successor), After: interruptionState(final), Verdict: stickiness(final),
	}, {
		Semantic: "what that turn's request carried", Authority: "control, projected per request",
		Artifact: "none: derived, never stored", Reconstruction: "the interruption block, if one still stands",
		Before: fmt.Sprintf("%d block(s)", successor.ModelSeesInterruption),
		After:  fmt.Sprintf("%d block(s)", final.ModelSeesInterruption), Verdict: verdictStable,
	}, {
		Semantic: "how the barrier ended", Authority: "the adjudication log",
		Artifact: "<stem>.adjudication.jsonl", Reconstruction: "an edge is written only for a barrier a turn received",
		Before: journalSummary(successor), After: journalSummary(final), Verdict: verdictStable,
	}, {
		Semantic: "the effect the dead decision held back", Authority: "the workspace",
		Artifact: final.Deferred.MarkerPath, Reconstruction: "no successor turn may release it",
		Before: ranOrNot(successor), After: ranOrNot(final), Verdict: heldBack(successor, final),
	}}
}

func interruptionState(o Observation) string {
	if len(o.Interrupted) == 0 {
		return "settled"
	}
	return fmt.Sprintf("%d still interrupted", len(o.Interrupted))
}

// stickiness is the question these arms were built to ask: does an
// interruption outlive the work that replaced it? A successor that took it over
// leaves nothing behind for later requests to carry.
func stickiness(final Observation) string {
	if len(final.Interrupted) == 0 && final.ModelSeesInterruption == 0 {
		return "inherited"
	}
	if len(final.Interrupted) == 0 {
		return "sticky context without an owner"
	}
	return "sticky"
}

func journalSummary(o Observation) string {
	var out []string
	for _, e := range o.Journal {
		switch {
		case e.Open():
			out = append(out, e.ID+":open")
		case e.SupersededBy != "":
			out = append(out, e.ID+":"+e.Disposition+" by turn "+e.SupersededBy)
		default:
			out = append(out, e.ID+":"+e.Disposition)
		}
	}
	return orNone(join(out))
}

func heldBack(successor, final Observation) string {
	if successor.Deferred.Executed || final.Deferred.Executed {
		return verdictViolated
	}
	return verdictHolds
}
