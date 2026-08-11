package control

import (
	"context"

	"reasonix/internal/tool"
)

// bindTurnScope binds a Goal turn's usage recorder, whose span stays active
// until the FSM commits. Neither chat nor Goal gets a round ceiling: rounds
// carry no information the spend axes lack, and a turn that reaches a high
// count without crossing them is one whose rounds are cheap and fast. An
// explicit max_steps still owns either turn.
func (c *Controller) bindTurnScope(ctx context.Context, continuation *goalContinuationSnapshot) context.Context {
	goalScopeID, goalScoped := c.goals.goalScopeIDForTurn(continuation)
	if !goalScoped {
		return ctx
	}
	recorder := c.goals.newTurnRecorder(goalScopeID, c.goals.continuationToken())
	c.goalUsageTee.setActiveRecorder(recorder)
	return tool.WithGoalTurnRecorder(ctx, recorder)
}
