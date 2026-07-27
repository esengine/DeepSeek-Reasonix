package agent

import (
	"fmt"
	"strings"
)

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
