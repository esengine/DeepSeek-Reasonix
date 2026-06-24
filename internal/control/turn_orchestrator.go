package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/jobs"
)

// turnOrchestrator owns foreground turn execution while Controller keeps the
// public ports, run-state guard, and session-scoped dependencies.
type turnOrchestrator struct {
	c *Controller
}

func newTurnOrchestrator(c *Controller) *turnOrchestrator {
	return &turnOrchestrator{c: c}
}

func (o *turnOrchestrator) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	c := o.c
	c.maybeSessionStart(ctx)
	c.maybeAutoPlan(ctx, raw)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, c.inputImages(input))
	input = c.Compose(input)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	defer c.recordDisplayForNewUser(startMessages, display)
	// Open a checkpoint for this turn before the user message is appended, so the
	// recorded message boundary precedes it and pre-edit snapshots land here.
	c.beginCheckpoint(input)
	// UserPromptSubmit / Stop hooks bracket the whole turn (incl. the plan
	// research + approved-execution sub-turns below): a gating UserPromptSubmit
	// aborts before any model call; Stop fires once when the turn returns.
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil // the hook's notify callback already surfaced the reason
		}
		defer func() { c.hooks.Stop(ctx, lastAssistantText(c.History()), turn) }()
	}
	if err := c.runner.Run(ctx, input); err != nil {
		return err
	}
	c.mu.Lock()
	plan := c.planMode
	c.mu.Unlock()
	if !plan {
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, planApprovalTool, "", nil)
	if err != nil {
		return err
	}
	if !allow {
		return nil // keep planning; plan mode stays on
	}
	c.SetPlanMode(false)
	todoArgs := c.seedPlanTodos(proposal)
	execStart := c.sessionMessageCount()
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only; a
	// later turn (even "continue") falls back to the normal per-tool approval.
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	if err := c.runner.Run(ctx, c.ComposeSynthetic(planApprovedMessage)); err != nil {
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}

func (o *turnOrchestrator) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	if err := o.runTurnWithRawDisplay(ctx, input, raw, display); err != nil {
		if ctx.Err() != nil {
			o.c.stopGoal(GoalStatusStopped)
		}
		return err
	}
	return o.continueGoal(ctx)
}

func (o *turnOrchestrator) continueGoal(ctx context.Context) error {
	c := o.c
	for {
		cont := o.advanceGoalAfterTurn()
		if !cont {
			return nil
		}
		if err := ctx.Err(); err != nil {
			c.stopGoal(GoalStatusStopped)
			return err
		}
		turn := goalContinueTurn
		if msg, ok := c.goals.takeIntercept(); ok {
			turn = msg
			c.notice("goal intercept: incomplete todos remain (override with a second [goal:complete])")
		}
		if err := o.runTurnWithRawDisplay(ctx, turn, turn, ""); err != nil {
			if ctx.Err() != nil {
				c.stopGoal(GoalStatusStopped)
			}
			return err
		}
	}
}

func (o *turnOrchestrator) advanceGoalAfterTurn() bool {
	c := o.c
	// Gather every input the FSM needs off the goal lock: parse the marker,
	// snapshot the executor's todos + readiness, and check tool activity. None
	// of these touch goal state, so the machine's critical section stays pure.
	status, reason, _ := parseGoalStatusMarker(lastAssistantText(c.History()))
	var readiness string
	if c.executor != nil {
		readiness = c.executor.GoalReadinessFailure()
	}
	res := c.goals.advance(goalAdvanceInput{
		status:     status,
		reason:     reason,
		toolCalled: c.toolWasCalledLastTurn(),
		todos:      c.goalTodos(),
		readiness:  readiness,
	})
	c.persistGoalState(res.path, res.data, res.ok)
	if res.notice != "" {
		c.notice(res.notice)
	}
	return res.cont
}
