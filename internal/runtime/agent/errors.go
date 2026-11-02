package agent

import (
	"errors"
	"fmt"
)

// PauseClass names the guard that deliberately ended a run, so a host can
// classify an outcome without reaching into the unexported pause types.
// Empty for ordinary provider/tool failures.
func PauseClass(err error) string {
	var budgetPause *taskBudgetPause
	if errors.As(err, &budgetPause) {
		return "task_budget"
	}
	var maxSteps *maxStepsPause
	if errors.As(err, &maxSteps) {
		return "max_steps"
	}
	var stalled *noProgressPause
	if errors.As(err, &stalled) {
		return PauseKindNoProgress
	}
	var repeated *perseverationPause
	if errors.As(err, &repeated) {
		return PauseKindPerseveration
	}
	return ""
}

// RunPauseInfo is the stable host-facing description of a deliberate Run
// boundary. It keeps unexported control-flow error types private while allowing
// Controller to distinguish a host default from an explicit user max_steps.
type RunPauseInfo struct {
	Kind      string
	Limit     int
	Key       string
	HostOwned bool
	Reason    string
}

// InspectRunPause unwraps a deliberate explicit run boundary.
func InspectRunPause(err error) (RunPauseInfo, bool) {
	var maxSteps *maxStepsPause
	if errors.As(err, &maxSteps) {
		return RunPauseInfo{Kind: "max_steps", Limit: maxSteps.steps, Key: maxSteps.key, HostOwned: maxSteps.hostOwned}, true
	}
	var budget *taskBudgetPause
	if errors.As(err, &budget) {
		return RunPauseInfo{Kind: "task_budget", Key: budget.axis, HostOwned: true, Reason: budget.detail}, true
	}
	var stalled *noProgressPause
	if errors.As(err, &stalled) {
		return RunPauseInfo{Kind: PauseKindNoProgress, Limit: stalled.limit, Key: stalled.key, Reason: stalled.detail}, true
	}
	var repeated *perseverationPause
	if errors.As(err, &repeated) {
		return RunPauseInfo{Kind: PauseKindPerseveration, Key: repeated.key, Reason: repeated.detail}, true
	}
	return RunPauseInfo{}, false
}

// EmptyFinalError reports that the model ended its response with no visible
// answer text on every one of the host's retries. FinishReason is the last
// terminal the provider reported, so a caller can tell a response cut off at
// the output limit from one the model closed itself.
type EmptyFinalError struct {
	Blocks       int
	FinishReason string
}

func (e *EmptyFinalError) Error() string {
	if e == nil {
		return "model finished without a visible final answer"
	}
	msg := fmt.Sprintf("model finished without a visible final answer %d times", e.Blocks)
	if truncatedFinish(e.FinishReason) {
		msg += fmt.Sprintf(" (the last response %s)", truncationCause(e.FinishReason))
	}
	return msg
}
