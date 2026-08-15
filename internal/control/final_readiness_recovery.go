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
	defaultContinueChecksPrompt  = "Continue the remaining final checks, preserve completed work, and only finish after the host readiness requirements pass."
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
	return c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		if c.executor == nil || !c.executor.PrepareFinalReadinessRecovery() {
			return ErrNoFinalReadinessRecovery
		}
		return c.runTurn(runCtx, input)
	})
}

// SubmitFinalReadinessRecovery preserves the immediately preceding exhausted
// ledger for one explicit asynchronous continuation.
func (c *Controller) SubmitFinalReadinessRecovery(display, input string) {
	c.runGuarded(func(ctx context.Context) error {
		if c.executor == nil || !c.executor.PrepareFinalReadinessRecovery() {
			return ErrNoFinalReadinessRecovery
		}
		return c.runGoalLoopWithRawDisplay(ctx, input, input, display)
	})
}

// SubmitDeliveryRecovery preserves the v1.25 desktop/API symbol.
func (c *Controller) SubmitDeliveryRecovery(display, input string) {
	c.SubmitFinalReadinessRecovery(display, input)
}

func (c *Controller) submitFinalReadinessCommand(trimmed, display string) bool {
	prompt, ok := ParseFinalReadinessRecoveryCommand(trimmed)
	if !ok {
		return false
	}
	if strings.TrimSpace(display) == "" {
		display = trimmed
	}
	c.SubmitFinalReadinessRecovery(display, prompt)
	return true
}
