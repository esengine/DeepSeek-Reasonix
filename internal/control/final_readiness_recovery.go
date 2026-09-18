package control

import (
	"context"
	"strings"
)

const (
	// FinalReadinessRecoveryAction is the typed transport action used by HTTP
	// and ACP. It is additive and optional, so older clients remain compatible.
	FinalReadinessRecoveryAction = "final_readiness_recovery"
	ContinueChecksCommand        = "/continue-checks"
	defaultContinueChecksPrompt  = "Continue checking the work. Preserve completed changes, run relevant checks, and report the observed results and anything you could not verify."
)

// ParseFinalReadinessRecoveryCommand converts the explicit slash action into a
// model prompt. Optional trailing text is user guidance; the command token
// itself never reaches the provider.
func ParseFinalReadinessRecoveryCommand(input string) (prompt string, ok bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed != ContinueChecksCommand && !strings.HasPrefix(trimmed, ContinueChecksCommand+" ") {
		return "", false
	}
	prompt = strings.TrimSpace(strings.TrimPrefix(trimmed, ContinueChecksCommand))
	if prompt == "" {
		prompt = defaultContinueChecksPrompt
	}
	return prompt, true
}

// RunFinalReadinessRecovery is the synchronous transport-neutral recovery path.
func (c *Controller) RunFinalReadinessRecovery(ctx context.Context, input string) error {
	return c.RunFinalReadinessRecoveryWithAdmission(ctx, input, nil)
}

// RunFinalReadinessRecoveryWithAdmission is a retired compatibility action. Old
// history stays readable, but no checkpoint can authorize or replay work.
func (c *Controller) RunFinalReadinessRecoveryWithAdmission(ctx context.Context, input string, onAdmitted func()) error {
	return ErrNoFinalReadinessRecovery
}

// SubmitFinalReadinessRecovery retains the asynchronous symbol for old clients
// and emits the stable retirement error through the ordinary turn path.
func (c *Controller) SubmitFinalReadinessRecovery(display, input string) {
	c.submissions.mu.Lock()
	defer c.releaseSubmissionAdmission()
	c.submitFinalReadinessRecoveryLocked(display, input, turnAdmission{})
}

func (c *Controller) submitFinalReadinessRecoveryLocked(display, input string, admission turnAdmission) {
	c.runGuardedWithAdmission(func(ctx context.Context) error {
		return ErrNoFinalReadinessRecovery
	}, admission)
}

// SubmitDeliveryRecovery preserves the v1.25 desktop/API symbol.
func (c *Controller) SubmitDeliveryRecovery(display, input string) {
	c.SubmitFinalReadinessRecovery(display, input)
}

func (c *Controller) submitFinalReadinessCommand(trimmed, display string, admission turnAdmission) bool {
	prompt, ok := ParseFinalReadinessRecoveryCommand(trimmed)
	if !ok {
		return false
	}
	if strings.TrimSpace(display) == "" {
		display = trimmed
	}
	c.submitFinalReadinessRecoveryLocked(display, prompt, admission)
	return true
}
