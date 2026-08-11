package control

import (
	"context"

	"reasonix/internal/agent"
)

// bindWorkflowTurnContext installs the per-turn contracts shared by Plan and
// Goal. Goal's recorder remains active through evaluator commit so all billable
// usage is attributed to the same observational span.
func (o *turnOrchestrator) bindWorkflowTurnContext(ctx context.Context, continuation *goalContinuationSnapshot) (context.Context, bool) {
	c := o.c
	c.mu.Lock()
	requireVisibleFinal := c.planMode
	c.mu.Unlock()

	ctx, goalTurn := c.bindTurnScope(ctx, continuation)
	if goalTurn {
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
