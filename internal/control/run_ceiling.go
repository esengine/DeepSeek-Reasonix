package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

// bindTurnScope binds a turn's boundaries: the Goal run ceiling and the usage
// recorder whose span stays active until the FSM commits. Ordinary chat gets no
// round ceiling — rounds carry no information agent.TaskBudget's axes lack, and
// a turn that reaches a high count without crossing them is one whose rounds
// are cheap and fast. An explicit max_steps still owns either turn.
func (c *Controller) bindTurnScope(ctx context.Context, continuation *goalContinuationSnapshot) context.Context {
	goalScopeID, goalScoped := c.goals.goalScopeIDForTurn(continuation)
	if !goalScoped {
		return ctx
	}
	ctx = agent.WithDefaultRunStepLimit(ctx, goalRunRoundLimit, goalRunRoundKey)
	recorder := c.goals.newTurnRecorder(goalScopeID, c.goals.continuationToken())
	c.goalUsageTee.setActiveRecorder(recorder)
	return tool.WithGoalTurnRecorder(ctx, recorder)
}
