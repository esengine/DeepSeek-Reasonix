package control

import (
	"reasonix/internal/evidence"
	"reasonix/internal/goaleval"
)

// completeDecision is the FSM's verdict on a complete claim for this turn.
type completeDecision struct {
	accept         bool
	rejectReminder string
	rejectReason   string
}

func (g *goalMachine) completeDecision(in goalAdvanceInput, reportComplete, evaluatorComplete bool) completeDecision {
	if !reportComplete && !evaluatorComplete {
		return completeDecision{}
	}
	reminder := formatIncompleteTodos(in.todos, in.readiness.Reason)
	if reminder == "" {
		return completeDecision{accept: true}
	}
	reason := clipGoalReason("readiness missing: " + in.readiness.Reason)
	repeated := g.lastContinuationReason == reason && reason != ""
	if repeated && repeatedCompleteMayFinish(in.readiness.Missing, in.todos) {
		return completeDecision{accept: true}
	}
	return completeDecision{rejectReminder: reminder, rejectReason: reason}
}

// repeatedCompleteMayFinish reports whether a second identical complete claim
// should stop the Goal as done: leftover gaps are only checks the model already
// had a turn to run. Incomplete todos keep the Goal working.
func (g *goalMachine) applyContinue(in goalAdvanceInput, reportComplete, evaluatorComplete bool, complete completeDecision) (intercept, interceptNotice string) {
	switch {
	case reportComplete:
		intercept = complete.rejectReminder
		if intercept == "" {
			intercept = formatIncompleteTodos(in.todos, in.readiness.Reason)
		}
		interceptNotice = "Goal is not ready to complete yet; continuing the remaining work."
		g.lastContinuationReason = complete.rejectReason
	case in.report != nil && in.report.status == GoalStatusRunning:
		g.lastContinuationReason = clipGoalReason(in.report.reason)
		if in.report.nextAction != "" {
			intercept = in.report.nextAction
		}
	case len(in.readiness.Missing) > 0:
		intercept = formatIncompleteTodos(in.todos, in.readiness.Reason)
		interceptNotice = "Goal is not ready to complete yet; continuing the remaining work."
		g.lastContinuationReason = clipGoalReason("readiness missing: " + in.readiness.Reason)
	case evaluatorComplete:
		intercept = complete.rejectReminder
		if intercept == "" {
			intercept = formatIncompleteTodos(in.todos, in.readiness.Reason)
		}
		interceptNotice = "Goal is not ready to complete yet; continuing the remaining work."
		g.lastEvaluatorReason = clipGoalReason(in.evaluator.reason)
		g.lastContinuationReason = complete.rejectReason
	case in.evaluator != nil && in.evaluator.outcome == goaleval.OutcomeContinue:
		g.lastEvaluatorReason = clipGoalReason(in.evaluator.reason)
	}
	return intercept, interceptNotice
}

func repeatedCompleteMayFinish(missing []string, todos []evidence.TodoItem) bool {
	if len(evidence.IncompleteTodos(todos)) > 0 {
		return false
	}
	if len(missing) == 0 {
		return false
	}
	for _, id := range missing {
		switch id {
		case "project_check", "verification", "review", "signoff":
		default:
			return false
		}
	}
	return true
}
