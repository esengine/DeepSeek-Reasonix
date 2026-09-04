package main

import (
	"fmt"
	"strings"
)

// How a host can stand after dying with a question open. The names are the
// outcomes, not a pass and its failures: two of them are designs a host may
// legitimately choose, and only measurement says which one this is.
const (
	outcomeResumable   = "resumable"            // the question is still there to answer
	outcomeInterrupted = "interrupted-explicit" // it is gone, and the host says a turn was cut
	outcomeLostSilent  = "LOST-SILENT"          // it is gone and nothing records that it existed
)

// decisionRows classify what a restart inherited. The safety row comes first
// because it is the only one with a single acceptable answer: whether the
// decision survives is a design question, whether the effect it blocked stayed
// blocked is not.
func decisionRows(before, after Observation) []row {
	return []row{
		deferredRow(before, after),
		outcomeRow(before, after),
		barrierRow(before, after),
		modelContextRow(before, after),
		obligationRow(before, after),
	}
}

// deferredRow asserts the write the question held back never ran — not while
// the answer was pending, and not because a later process loaded the session.
func deferredRow(before, after Observation) row {
	verdict := verdictHolds
	if before.Deferred.Executed || after.Deferred.Executed {
		verdict = verdictViolated
	}
	return row{
		Semantic: "the effect the decision held back", Authority: "the workspace",
		Artifact: before.Deferred.MarkerPath, Reconstruction: "the blocked write never runs, before or after the boundary",
		Before: ranOrNot(before), After: ranOrNot(after), Verdict: verdict,
	}
}

func ranOrNot(o Observation) string {
	if o.Deferred.Executed {
		return "RAN"
	}
	return "did not run"
}

// outcomeRow names which of the three standings the restart produced. It is
// stated, not judged: a host that drops the question and records the
// interruption is a defensible design, and one that drops both is not.
func outcomeRow(before, after Observation) row {
	verdict := verdictHolds
	var outcome string
	switch {
	case len(after.Decisions) > 0:
		outcome = outcomeResumable
	case len(after.Interrupted) > 0:
		outcome = outcomeInterrupted
	case after.Obligation.InterruptionMarked > 0 || len(after.Obligation.UnansweredCalls) > 0:
		outcome = outcomeInterrupted
	default:
		outcome = outcomeLostSilent
		verdict = verdictViolated
	}
	return row{
		Semantic: "what the restart inherited", Authority: "approvalManager and the transcript",
		Artifact:       "none for the question; the transcript for the obligation",
		Reconstruction: "resumable, interrupted-explicit, or nothing at all",
		Before:         decisionSummary(before), After: outcome, Verdict: verdict,
	}
}

func decisionSummary(o Observation) string {
	if len(o.Decisions) == 0 {
		return "no decision open"
	}
	var kinds []string
	for _, d := range o.Decisions {
		kinds = append(kinds, string(d.Kind))
	}
	return strings.Join(kinds, ", ") + " open"
}

// barrierRow is the evidence behind an interrupted classification: a barrier
// the host recorded before anyone could be asked, still open, with no owner in
// this process waiting on it. It is deliberately not offered as answerable.
func barrierRow(before, after Observation) row {
	return row{
		Semantic: "durable adjudication barriers", Authority: "the adjudication log",
		Artifact: "<stem>.adjudication.jsonl", Reconstruction: "open records with no live owner in this process",
		Before: barrierSummary(before), After: barrierSummary(after), Verdict: verdictStable,
	}
}

func barrierSummary(o Observation) string {
	if len(o.Interrupted) == 0 {
		return "none"
	}
	var out []string
	for _, b := range o.Interrupted {
		out = append(out, b.Kind+":"+b.ID)
	}
	return join(out) + " (not answerable)"
}

// modelContextRow is the other read surface: what the next request tells the
// model. One block when an interruption stands, none when it does not, and it
// must never arrive as something answerable.
func modelContextRow(before, after Observation) row {
	verdict := verdictHolds
	if before.ModelSeesInterruption != expectedBlocks(before) || after.ModelSeesInterruption != expectedBlocks(after) {
		verdict = verdictViolated
	}
	return row{
		Semantic: "what the next request tells the model", Authority: "control, projected per request",
		Artifact: "none: derived, never stored", Reconstruction: "one block while an interruption stands, none otherwise",
		Before: modelContextSummary(before), After: modelContextSummary(after), Verdict: verdict,
	}
}

func expectedBlocks(o Observation) int {
	if len(o.Interrupted) > 0 {
		return 1
	}
	return 0
}

func modelContextSummary(o Observation) string {
	return fmt.Sprintf("%d block(s) for %d interruption(s)", o.ModelSeesInterruption, len(o.Interrupted))
}

// obligationRow shows the evidence behind that classification, so a reader can
// see what the host still says rather than trusting the label.
func obligationRow(before, after Observation) row {
	return row{
		Semantic: "what the transcript still owes", Authority: "the canonical transcript",
		Artifact: "<stem>.jsonl", Reconstruction: "tool calls with no result, and interruption records",
		Before: obligationSummary(before), After: obligationSummary(after), Verdict: verdictStable,
	}
}

func obligationSummary(o Observation) string {
	return fmt.Sprintf("unanswered %s, interruption records %d",
		orNone(join(o.Obligation.UnansweredCalls)), o.Obligation.InterruptionMarked)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
