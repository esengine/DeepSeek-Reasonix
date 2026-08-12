package agent

import (
	"reasonix/internal/event"
	"reasonix/internal/taskcontract"
	"reasonix/internal/taskpolicy"
)

// emitTurnPhase publishes a content-free host phase for the active turn.
func (a *Agent) emitTurnPhase(phase event.TurnPhaseName) {
	if a == nil || a.sink == nil || phase == "" {
		return
	}
	a.sink.Emit(event.Event{Kind: event.TurnPhase, PhaseName: phase, Text: string(phase)})
}

// emitCompletionSummary publishes the content-free end-of-turn quality summary
// when the turn mutated state or finished Partial/Blocked. Pure conversation
// and ordinary read-only success do not emit a quality card.
func (a *Agent) emitCompletionSummary(c *taskcontract.Contract) {
	if a == nil || a.sink == nil || c == nil {
		return
	}
	mutations := 0
	if a.evidence != nil {
		for _, r := range a.evidence.Receipts() {
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
	if a.turnPolicySet {
		switch a.turnPolicy.Review {
		case taskpolicy.ReviewNone:
			review = "none"
		default:
			if a.evidence != nil {
				if mut, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
					if a.evidence.HasSuccessfulReviewAfter(mut) {
						review = "passed"
					} else if a.turnPolicy.RequiresIndependentReview() {
						review = "unavailable"
					}
				}
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
	constraintDegraded := a.turnPolicySet && (a.turnPolicy.Constraints.ForbidTests || len(a.turnPolicy.Constraints.AllowedChecks) > 0)
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
	a.sink.Emit(event.Event{
		Kind: event.CompletionSummary,
		Completion: &event.CompletionSummaryInfo{
			Preset:             a.AgentPreset(),
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
