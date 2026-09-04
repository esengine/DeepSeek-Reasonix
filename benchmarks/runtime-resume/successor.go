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

// stickiness names the open question rather than answering it: an interruption
// that outlives the work replacing it is context every later request pays for.
func stickiness(final Observation) string {
	if len(final.Interrupted) == 0 {
		return outcomeInterrupted
	}
	return "sticky"
}

func heldBack(successor, final Observation) string {
	if successor.Deferred.Executed || final.Deferred.Executed {
		return verdictViolated
	}
	return verdictHolds
}
