package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

// bindTurnScope binds a Goal turn's spend budget and its usage recorder, the
// latter active until the FSM commits. Neither chat nor Goal gets a round
// ceiling: rounds carry no information the spend axes lack. An explicit
// max_steps still owns either turn.
func (c *Controller) bindTurnScope(ctx context.Context, continuation *goalContinuationSnapshot) context.Context {
	goalScopeID, goalScoped := c.goals.goalScopeIDForTurn(continuation)
	if !goalScoped {
		return ctx
	}
	ctx = agent.WithTaskBudget(ctx, c.goalTaskBudget())
	recorder := c.goals.newTurnRecorder(goalScopeID, c.goals.continuationToken())
	c.goalUsageTee.setActiveRecorder(recorder)
	return tool.WithGoalTurnRecorder(ctx, recorder)
}

// goalTaskBudget is the spend gate a Goal turn runs under: the shared budget
// plus the Goal-only token axis. Nothing here has a default — a Goal runs
// until it finishes, reaches a genuine blocker, or the user stops it. Anyone
// who wants a ceiling on an unattended loop sets one.
func (c *Controller) goalTaskBudget() agent.TaskBudget {
	b := c.taskBudget
	b.Tokens = c.goalTokenBudget
	return b
}
