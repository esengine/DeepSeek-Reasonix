package agent

import (
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

// summaryVerdictOf keeps the summary line and the completion receipt on one
// answer. A contract can be satisfied while the ledger still carries unproven
// work, and a line claiming complete beside a receipt listing gaps contradicts
// the same turn's own evidence.
func summaryVerdictOf(c *taskcontract.Contract, rep completion.Report) taskcontract.Verdict {
	verdict := c.GoalVerdict()
	if verdict == taskcontract.VerdictComplete && len(rep.Gaps) > 0 {
		return taskcontract.VerdictPartial
	}
	return verdict
}

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
func (a *Agent) emitCompletionSummary(c *taskcontract.Contract, rep completion.Report, blocked bool) {
	if a == nil || a.svc.sink == nil || c == nil {
		return
	}
	mutations := rep.Mutations
	verdict := summaryVerdictOf(c, rep)
	if blocked {
		// The gate, not the contract, had the last word on this turn.
		verdict = taskcontract.VerdictBlocked
	}
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
			switch {
			case a.task.ledger.HasSuccessfulReviewAfter(mut):
				review = "passed"
			case a.deliveryProfile && a.task.ledger.MutationRiskAfter(mut, a.projectSensitivePaths) >= evidence.RiskMedium:
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
			Preset:            a.AgentPreset(),
			Verdict:           summaryVerdict,
			Mutations:         mutations,
			ChecksPassed:      passed,
			ChecksFailed:      failed,
			ChecksSuppressed:  suppressed,
			Review:            review,
			GapKinds:          gaps,
			CriteriaRewritten: a.task.ledger.RewrittenCriteria(),
		},
	})
}
