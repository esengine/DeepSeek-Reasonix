package agent

import (
	"reasonix/internal/evidence"
	"reasonix/internal/plancontract"
)

// planTransitionDiff summarises a task-list rewrite by pairing steps on their
// stable ids; the task list is the plan's projection, so one comparison serves
// both layers. It returns "" when either side predates step ids, since pairing
// a list without them reads every step as replaced.
func planTransitionDiff(before, after []evidence.TodoItem) string {
	from, ok := planFromTodos(before, 1)
	if !ok {
		return ""
	}
	to, ok := planFromTodos(after, 2)
	if !ok {
		return ""
	}
	diff := plancontract.Compare(from, to)
	if !diff.Moved() {
		return ""
	}
	return plancontract.RenderDiff(diff)
}

// planFromTodos rebuilds the projection's shape. Only identity and hierarchy
// matter here — the comparison never looks at the fields a todo cannot carry.
func planFromTodos(todos []evidence.TodoItem, revision int) (plancontract.Plan, bool) {
	plan := plancontract.Plan{Objective: "task list", Revision: revision}
	phase := ""
	for _, todo := range todos {
		if todo.StepID == "" {
			return plancontract.Plan{}, false
		}
		step := plancontract.Step{ID: todo.StepID, Title: todo.Content}
		if todo.Level == 1 {
			step.ParentID = phase
		} else {
			phase = todo.StepID
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan, len(plan.Steps) > 0
}

// recoveryProposal assembles what the isolated reviewer is shown for one call.
// It lives beside the plan diff because that is the field most likely to grow.
func (a *Agent) recoveryProposal(plan *toolCallPlan, episodeID, subject, preview string) RecoveryProposal {
	return RecoveryProposal{
		AgentID:        a.recoveryAgentID,
		TaskID:         a.recoveryTaskID,
		TaskScopeID:    recoveryTaskScopeID(a.deliveryScopeID, a.recoveryRunSeq.Load()),
		EpisodeID:      episodeID,
		TaskSummary:    a.recoveryTaskSummary,
		Tool:           plan.evidenceName,
		Args:           plan.evidenceArgs,
		Subject:        subject,
		Preview:        preview,
		ReadOnly:       plan.readOnly,
		Mutates:        plan.mutates,
		Verification:   plan.verification,
		PlanTransition: plan.planTransition,
		// The existing rule asks whether a retry drifts from the call that
		// failed. This asks the question the user actually approved an answer
		// to: is this write outside the plan they agreed on.
		ExpandedScope: plan.mutates && a.mutationEscapesPlan(plan.evidenceName, plan.evidenceArgs),
		PlanBefore:    plan.planBefore,
		PlanAfter:     plan.planAfter,
		PlanDiff:      plan.planDiff,
	}
}
