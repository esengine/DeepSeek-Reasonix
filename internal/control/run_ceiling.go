package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

// chatRunRoundLimit is a backstop, not a budget. Judging when a turn stopped
// earning its rounds belongs to the no-progress ladder, which covers the
// wandering shape since evidence.explorationRunLimit; this only bounds a turn
// whose judgement is wrong in a shape nobody has seen yet. Rounds count per
// Run, so a "continue" starts them over — it does not bound a whole task.
const (
	chatRunRoundLimit = 100
	chatRunRoundKey   = "chat model rounds"
)

// bindTurnScope installs the turn's Run ceiling and, for a Goal turn, the usage
// recorder whose span stays active until the FSM commits. An explicit max_steps
// still owns either turn.
func (c *Controller) bindTurnScope(ctx context.Context, continuation *goalContinuationSnapshot) context.Context {
	goalScopeID, goalScoped := c.goals.goalScopeIDForTurn(continuation)
	if !goalScoped {
		return agent.WithDefaultRunStepLimit(ctx, chatRunRoundLimit, chatRunRoundKey)
	}
	ctx = agent.WithDefaultRunStepLimit(ctx, goalRunRoundLimit, goalRunRoundKey)
	recorder := c.goals.newTurnRecorder(goalScopeID, c.goals.continuationToken())
	c.goalUsageTee.setActiveRecorder(recorder)
	return tool.WithGoalTurnRecorder(ctx, recorder)
}
