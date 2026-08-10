package agent

import (
	"errors"
	"fmt"
	"strings"
)

// PauseClass names the guard that deliberately ended a run, so a host can
// classify an outcome without reaching into the unexported pause types.
// Empty for ordinary provider/tool failures.
func PauseClass(err error) string {
	var maxSteps *maxStepsPause
	if errors.As(err, &maxSteps) {
		return "max_steps"
	}
	var stall *todoStallPause
	if errors.As(err, &stall) {
		return "todo_stall"
	}
	var stuck *goalStuckPause
	if errors.As(err, &stuck) {
		return "goal_stuck"
	}
	var readiness *FinalReadinessError
	if errors.As(err, &readiness) {
		return "final_readiness"
	}
	var recovery *RecoveryPauseError
	if errors.As(err, &recovery) {
		return "recovery_paused"
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

// InspectRunPause unwraps a deliberate max-round or Goal-stuck pause.
func InspectRunPause(err error) (RunPauseInfo, bool) {
	var maxSteps *maxStepsPause
	if errors.As(err, &maxSteps) {
		return RunPauseInfo{Kind: "max_steps", Limit: maxSteps.steps, Key: maxSteps.key, HostOwned: maxSteps.hostOwned}, true
	}
	var stuck *goalStuckPause
	if errors.As(err, &stuck) {
		return RunPauseInfo{Kind: "goal_stuck", Limit: stuck.limit, Key: stuck.key, HostOwned: true, Reason: stuck.reason}, true
	}
	var stall *todoStallPause
	if errors.As(err, &stall) {
		return RunPauseInfo{Kind: "todo_stall", Limit: stall.rounds, Key: "todo progress", HostOwned: true, Reason: "the current todo made no host-observed progress"}, true
	}
	return RunPauseInfo{}, false
}

// FinalReadinessError reports that the model exhausted its recovery attempts
// before satisfying the host-observed delivery checks.
type FinalReadinessError struct {
	Attempts int
	Reason   string
	Missing  []string
}

func (e *FinalReadinessError) Error() string {
	if e == nil {
		return "final-answer readiness failed"
	}
	return fmt.Sprintf("final-answer readiness failed %d times: %s", e.Attempts, e.Reason)
}

// RecoveryPauseError reports that Auto recovery exhausted its Episode budget
// and the model either summarized or continued calling tools after the one-shot
// finalization round. It is a control-flow signal, not a provider failure:
// completed work is kept and the user can continue in the next message.
type RecoveryPauseError struct {
	// Message is the user-facing English product copy for wire/CLI clients.
	Message string
	// StopReason is an internal classifier; never show it as product copy.
	StopReason string
	// Detail is optional expandable diagnostic text (last error / counts).
	Detail string
}

func (e *RecoveryPauseError) Error() string {
	if e == nil {
		return "automatic retries paused"
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return "Automatic retries paused. Reasonix stopped repeated attempts and kept completed work. Send \"continue\" to start a fresh attempt, or add instructions to change direction."
}
