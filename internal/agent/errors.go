package agent

import (
	"errors"
	"fmt"
	"strings"
)

// ReasoningReplayFailure classifies why an assistant turn could not safely be
// committed to provider-visible history.
type ReasoningReplayFailure string

const (
	ReasoningReplayMissing      ReasoningReplayFailure = "missing_reasoning"
	ReasoningReplayOverflow     ReasoningReplayFailure = "reasoning_overflow"
	ReasoningReplayUnreplayable ReasoningReplayFailure = "unreplayable_history"
)

// ReasoningReplayError stops client tools before execution when their provider
// reasoning cannot be replayed. Completed work is retained as LocalOnly by the
// ordinary interrupted-turn recovery path.
type ReasoningReplayError struct {
	Kind ReasoningReplayFailure
}

func (e *ReasoningReplayError) Error() string {
	if e != nil && e.Kind == ReasoningReplayOverflow {
		return "The provider reasoning exceeded the client safety limit, so Reasonix did not run the requested tools. Existing work was kept; retry to continue safely."
	}
	return "The provider omitted reasoning required to replay this tool turn, so Reasonix did not run the requested tools. Existing work was kept; retry to continue safely."
}

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
// Controller to distinguish task budgets from an explicit runtime max_steps.
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
		return RunPauseInfo{Kind: "max_steps", Limit: maxSteps.steps, Key: maxSteps.key}, true
	}
	var budget *taskBudgetPause
	if errors.As(err, &budget) {
		return RunPauseInfo{Kind: "task_budget", Key: budget.axis, HostOwned: true, Reason: budget.detail}, true
	}
	return RunPauseInfo{}, false
}

// ReadinessContinuationClass tells a host whether an observed readiness gap is
// safe and concrete enough for a bounded synthetic follow-up turn.
type ReadinessContinuationClass string

const (
	// ReadinessContinuationNone is also the zero value so older callers that
	// construct FinalReadinessError directly never opt into another model turn.
	ReadinessContinuationNone ReadinessContinuationClass = ""
	// ReadinessContinuationGeneric covers ordinary post-write verification and
	// review gaps. Hosts may give it one bounded follow-up turn.
	ReadinessContinuationGeneric ReadinessContinuationClass = "generic"
	// ReadinessContinuationHighConfidence covers exact or strict, safely
	// actionable readiness duties. Hosts may give it a second turn only after
	// host-observable progress.
	ReadinessContinuationHighConfidence ReadinessContinuationClass = "high_confidence"
)

// FinalReadinessError reports that the model exhausted its recovery attempts
// before satisfying the host-observed delivery checks.
type FinalReadinessError struct {
	Attempts          int
	Reason            string
	Missing           []string
	ContinuationClass ReadinessContinuationClass
	ProgressKey       string
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
