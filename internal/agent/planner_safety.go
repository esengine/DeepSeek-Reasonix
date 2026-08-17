package agent

import (
	"errors"
	"fmt"
)

// A host-owned emergency or task budget must not strand an ordinary task. If
// the planner ignores its finalization nudge, the executor still owns the task
// and can inspect the workspace directly. Explicit no-execution and approval
// boundaries remain fail-closed.
const (
	plannerSafetyFallbackNotice = "Planner stopped at a safety boundary without a final plan; continuing this turn with the executor."
	plannerSafetyBoundaryError  = "planner could not finalize before a safety boundary; no execution was started"
)

func plannerSafetyPauseDetail(err error) string {
	var maxPause *maxStepsPause
	if errors.As(err, &maxPause) {
		return fmt.Sprintf(
			"planner did not finalize after %d emergency-bounded tool-call rounds (%s) and one finalization round",
			maxPause.steps,
			maxPause.key,
		)
	}
	var budgetPause *taskBudgetPause
	if errors.As(err, &budgetPause) {
		return fmt.Sprintf("planner did not finalize before the task's %s budget (%s)", budgetPause.axis, budgetPause.detail)
	}
	return "planner stopped before producing a final plan"
}
