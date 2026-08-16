package agent

import (
	"slices"

	"reasonix/internal/agentpreset"
	"reasonix/internal/completion"
	"reasonix/internal/event"
	"reasonix/internal/taskcontract"
)

// emitTurnPhase publishes a content-free host phase for the active turn.
func (a *Agent) emitTurnPhase(phase event.TurnPhaseName) {
	if a == nil || a.svc.sink == nil || phase == "" {
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.TurnPhase, PhaseName: phase, Text: string(phase)})
}

// emitCompletionSummary publishes the content-free end-of-turn quality summary
// when the turn mutated state or finished Partial/Blocked. Pure conversation
// and ordinary read-only success do not emit a quality card.
func (a *Agent) emitCompletionSummary(c *taskcontract.Contract, report completion.Report) {
	if a == nil || a.svc.sink == nil || c == nil {
		return
	}
	mutations := 0
	if a.task.ledger != nil {
		for _, r := range a.task.ledger.Receipts() {
			if r.Success && (r.Mutation || r.Write) {
				mutations++
			}
		}
	}
	verdict := c.GoalVerdict()
	// Skip noise: no mutations and ordinary complete/continue conversation.
	if mutations == 0 && (verdict == taskcontract.VerdictComplete || verdict == taskcontract.VerdictContinue || verdict == taskcontract.VerdictUncertain) {
		if !c.HasSuppressed() {
			return
		}
	}
	passed, failed, suppressed := 0, 0, 0
	for _, check := range c.Checks {
		switch check.Status {
		case taskcontract.Satisfied:
			passed++
		case taskcontract.Failed:
			failed++
		case taskcontract.Suppressed:
			suppressed++
		}
	}
	review := "none"
	if a.task.ledger != nil {
		if mut, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
			if a.task.ledger.HasSuccessfulReviewAfter(mut) {
				review = "passed"
			} else if a.requiresIndependentReview() {
				review = "unavailable"
			}
		}
	}
	var gaps []string
	if c.HasSuppressed() {
		gaps = append(gaps, "suppressed")
	}
	for _, check := range c.Checks {
		if check.Status == taskcontract.Stale {
			gaps = append(gaps, "stale_check")
			break
		}
	}
	for _, req := range c.Requirements {
		if req.Required && req.Status == taskcontract.Suppressed {
			gaps = append(gaps, "suppressed_requirement")
			break
		}
	}
	gaps = completionGapKinds(gaps, report)
	constraintDegraded := a.turn.constraints.ForbidTests || len(a.turn.constraints.AllowedChecks) > 0
	summaryVerdict := verdict.String()
	switch verdict {
	case taskcontract.VerdictComplete:
		summaryVerdict = "complete"
	case taskcontract.VerdictPartial:
		summaryVerdict = "partial"
	case taskcontract.VerdictBlocked:
		summaryVerdict = "blocked"
	case taskcontract.VerdictContinue:
		summaryVerdict = "continue"
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.CompletionSummary,
		Completion: &event.CompletionSummaryInfo{
			// Preset is a deprecated wire-compat field: it is pinned to the
			// historical default so one-version-old clients keep parsing. New
			// surfaces read the verdict/check/review/gap fields instead.
			Preset:             string(agentpreset.Balanced),
			Verdict:            summaryVerdict,
			Mutations:          mutations,
			ChecksPassed:       passed,
			ChecksFailed:       failed,
			ChecksSuppressed:   suppressed,
			Review:             review,
			GapKinds:           gaps,
			ConstraintDegraded: constraintDegraded,
		},
	})
}

func completionGapKinds(gaps []string, report completion.Report) []string {
	for _, gap := range report.Gaps {
		kind := gap.Kind.String()
		if !slices.Contains(gaps, kind) {
			gaps = append(gaps, kind)
		}
	}
	return gaps
}
