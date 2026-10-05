package control

import (
	"fmt"

	"reasonix/internal/base/i18n"
	"reasonix/internal/runtime/goaleval"
)

// The Controller's goal verbs: what a frontend calls to set, pause, resume and
// clear a goal, and what the turn loop calls to stop one. The state machine
// they drive is goalMachine (goal.go); these own the orchestration around it —
// persistence, notices, and the executor's view of the current goal.

func (c *Controller) stopGoal(status string) {
	path, data, ok := c.goals.stop(status, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// GoalStrict enables or disables strict goal mode. Since the structured
// protocol, every complete claim is validated against host readiness and an
// incomplete-todo intercept can never be overridden, so the flag is persisted
// for compatibility with older frontends but no longer changes FSM behavior.
func (c *Controller) GoalStrict(strict bool) {
	path, data, ok := c.goals.setStrict(strict, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// SetGoal stores a session-scoped active goal. Compose injects it into outgoing
// user turns, not the system prompt or tool schema, so it does not disturb the
// cache-stable prefix.
func (c *Controller) SetGoal(goal string) {
	c.SetGoalWithResearchMode(goal, GoalResearchAuto)
}

// SetGoalDurable updates the Goal only when its sidecar can be replaced
// atomically.
func (c *Controller) SetGoalDurable(goal string) error {
	snapshot := c.goals.capture()
	path, data, persist := c.goals.set(goal, budgetClassForLegacyMode(goal, GoalResearchAuto), c.frozenVerificationContract(), c.goalTodos())
	if persist {
		if err := c.goals.writeStateErr(path, data); err != nil {
			c.goals.restore(snapshot)
			return err
		}
	}
	return nil
}

func (c *Controller) SetGoalWithResearchMode(goal string, researchMode GoalResearchMode) {
	path, data, ok := c.goals.set(goal, budgetClassForLegacyMode(goal, researchMode), c.frozenVerificationContract(), c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// ResumeGoal re-enters a recoverable blocked/stopped Goal without resetting its
// delivery evidence scope or accumulated usage statistics.
func (c *Controller) ResumeGoal() bool {
	spentBudget := c.goals.runtimeView().StopCause == stopCauseBudgetSpend
	path, data, persist, resumed := c.goals.resume(c.goalTodos())
	if !resumed {
		return false
	}
	c.persistGoalState(path, data, persist)
	if c.executor != nil {
		if spentBudget {
			c.executor.ResetTaskBudget()
		}
		c.executor.RestoreDeliveryCheckpoint(c.goals.deliveryState())
	}
	return true
}

// PauseGoal suspends a running Goal without losing its todo list, Delivery
// checkpoint, or runtime history; ResumeGoal restores it. Returns false when no
// running Goal exists.
func (c *Controller) PauseGoal() bool {
	if !c.goals.active() {
		return false
	}
	path, data, ok := c.goals.pauseFor(stopCauseManual, i18n.M.GoalPausedReason, c.goalTodos())
	c.persistGoalState(path, data, ok)
	c.notice(i18n.M.GoalPaused)
	return true
}

// GoalRuntime returns the active Goal's usage/runtime summary for frontends.
func (c *Controller) GoalRuntime() GoalRuntimeView {
	return c.goals.runtimeView()
}

// goalEvaluatorEvidence assembles the bounded evaluator's evidence: the goal
// contract, the current assistant final, a todo/readiness summary,
// turn/budget state, and the last
// continuation reason. Every field is treated as untrusted by the evaluator.
func (c *Controller) goalEvaluatorEvidence() goaleval.GoalEvidence {
	goal, _ := c.goals.snapshot()
	ev := goaleval.GoalEvidence{
		GoalContract:           goal,
		LastContinuationReason: c.goals.lastContinuationReasonText(),
	}
	if c.executor != nil {
		ev.AssistantFinal = lastAssistantText(c.History())
		todos := c.goalTodos()
		incomplete := 0
		for _, t := range todos {
			if t.Status != "completed" {
				incomplete++
			}
		}
		rr := c.executor.ReadinessResult()
		readinessText := "ready"
		if rr.Reason != "" {
			readinessText = rr.Reason
		}
		ev.TodoSummary = fmt.Sprintf("todos: %d total, %d incomplete; delivery readiness: %s", len(todos), incomplete, readinessText)
	}
	ev.TurnStatus = c.goals.budgetStatusText()
	return ev
}

func (c *Controller) persistGoalDeliveryCheckpoint() {
	if c.executor == nil {
		return
	}
	checkpoint := c.executor.DeliveryCheckpoint()
	path, data, ok := c.goals.setDeliveryCheckpoint(checkpoint, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

func (c *Controller) ClearGoal() {
	c.SetGoal("")
}

func (c *Controller) Goal() string {
	return c.goals.goalText()
}

func (c *Controller) GoalStatus() string {
	return c.goals.statusForDisplay()
}
