package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/tool"
)

// bindWorkflowTurnContext installs the per-turn contracts shared by Plan and
// Goal. Goal's recorder remains active through evaluator commit so all billable
// usage is attributed to the same observational span.
func (o *turnOrchestrator) bindWorkflowTurnContext(ctx context.Context, continuation *goalContinuationSnapshot) (context.Context, bool) {
	c := o.c
	c.mu.Lock()
	requireVisibleFinal := c.planMode
	c.mu.Unlock()

	goalScopeID, goalTurn := c.goals.goalScopeIDForTurn(continuation)
	if goalTurn {
		recorder := c.goals.newTurnRecorder(goalScopeID, c.goals.continuationToken())
		if c.executor != nil {
			recorder.setProgressBefore(c.executor.HostProgressSignature())
		}
		ctx = tool.WithGoalTurnRecorder(ctx, recorder)
		c.goalUsageTee.setActiveRecorder(recorder)
		requireVisibleFinal = true
	}
	if requireVisibleFinal {
		ctx = agent.WithRequireVisibleFinal(ctx)
	}
	return ctx, goalTurn
}

func (o *turnOrchestrator) captureGoalTurnFinal(goalTurn bool, boundary turnFinalBoundary) {
	if goalTurn {
		o.lastTurnFinal = boundary.currentVisibleFinal(o.c)
	}
}
