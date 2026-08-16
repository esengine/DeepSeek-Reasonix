package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/evidence"
	"reasonix/internal/i18n"
	"reasonix/internal/sessioninbox"
)

const (
	readinessGenericTurns        = 1
	readinessHighConfidenceTurns = 2
)

func readinessContinuationBudget(class agent.ReadinessContinuationClass) int {
	switch class {
	case agent.ReadinessContinuationGeneric:
		return readinessGenericTurns
	case agent.ReadinessContinuationHighConfidence:
		return readinessHighConfidenceTurns
	default:
		return 0
	}
}

func readinessMadeProgress(previous, current string) bool {
	return previous != "" && current != "" && previous != current
}

func finalReadinessWithAttempts(err error, attempts int) error {
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) || readinessErr == nil {
		return err
	}
	copy := *readinessErr
	copy.Missing = append([]string(nil), readinessErr.Missing...)
	copy.Attempts = attempts
	return &copy
}

// hasPendingUserWork reads only already-owned state and an already open inbox
// Store. It never creates an inbox or holds a Controller lock while taking an
// Agent/Store lock. A busy or unreadable durable inbox conservatively yields to
// potential user work.
func (c *Controller) hasPendingUserWork() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	canceling := c.canceling
	parked := len(c.parkedTurns) > 0
	executor := c.executor
	c.mu.Unlock()
	if canceling || parked {
		return true
	}
	if executor != nil && executor.HasUnappliedSteer() {
		return true
	}
	c.inbox.mu.Lock()
	store := c.inbox.store
	c.inbox.mu.Unlock()
	if store == nil {
		return false
	}
	snapshot, err := store.TryFreshSnapshot()
	if err != nil {
		return true
	}
	if snapshot.Readonly {
		return true
	}
	for _, item := range snapshot.Items {
		if item.RunID != "" && item.RunID != sessioninbox.ProcessRunID() {
			return true
		}
		switch item.State {
		case sessioninbox.StateQueued, sessioninbox.StateBlocked,
			sessioninbox.StateUncertain, sessioninbox.StateSteerAccepted:
			return true
		}
	}
	return false
}

// continueUntilReady runs known missing requirements as synthetic follow-up
// turns. The returned error is the last turn's outcome, so callers see one
// foreground operation regardless of how many bounded continuations it used.
func (o *turnOrchestrator) continueUntilReady(ctx context.Context, turnErr error) error {
	var initial *agent.FinalReadinessError
	if !errors.As(turnErr, &initial) || initial == nil {
		return turnErr
	}
	budget := readinessContinuationBudget(initial.ContinuationClass)
	if budget == 0 {
		return turnErr
	}
	initialAttempts := max(initial.Attempts, 1)
	automaticTurns := 0
	previousProgress := initial.ProgressKey
	for automaticTurns < budget {
		var readinessErr *agent.FinalReadinessError
		if !errors.As(turnErr, &readinessErr) || readinessErr == nil {
			return turnErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if o.c.CancelRequested() {
			return context.Canceled
		}
		if o.c.hasPendingUserWork() {
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		if readinessContinuationBudget(readinessErr.ContinuationClass) == 0 {
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		if automaticTurns > 0 && !readinessMadeProgress(previousProgress, readinessErr.ProgressKey) {
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		prompt := readinessContinuationPrompt(o.c.goalTodos(), readinessErr.Reason)
		if prompt == "" {
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		// Preserve the finished turn's receipts. Starting with an empty ledger
		// would make the gap disappear because its evidence was dropped, not
		// because the remaining checks actually passed.
		if o.c.executor == nil || !o.c.executor.PrepareFinalReadinessRecovery() {
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		o.c.notice(i18n.M.ReadinessContinuing)
		previousProgress = readinessErr.ProgressKey
		nextErr := o.runOrchestratedTurn(ctx, orchestratedTurn{
			input: prompt, raw: prompt, synthetic: true,
		})
		if o.c.executor.RestoreFinalReadinessRecoveryPreparation() {
			// input.receive, PromptSubmit, or another host preflight ended before
			// Agent.Run consumed the preparation. Preserve the recovery card and
			// do not count a model turn that never happened as an attempt.
			if nextErr != nil {
				return nextErr
			}
			return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
		}
		automaticTurns++
		turnErr = nextErr
		if turnErr == nil {
			return nil
		}
	}
	return finalReadinessWithAttempts(turnErr, initialAttempts+automaticTurns)
}

// readinessContinuationPrompt states only host-observed missing work. It is an
// append-only user turn, leaving the system prompt and tool-schema cache prefix
// unchanged.
func readinessContinuationPrompt(todos []evidence.TodoItem, reason string) string {
	var parts []string
	if incomplete := evidence.IncompleteTodos(todos); len(incomplete) > 0 {
		var b strings.Builder
		b.WriteString("these tasks are still incomplete:")
		for _, todo := range incomplete {
			fmt.Fprintf(&b, "\n  - %s (%s)", todo.Content, todo.Status)
		}
		parts = append(parts, b.String())
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		parts = append(parts, reason)
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(agent.ReadinessContinuationPrefix + "\n")
	for _, part := range parts {
		b.WriteString("- " + part + "\n")
	}
	b.WriteString("Address only the readiness items above within the original request. Do not expand scope or repeat destructive or external actions. If an item cannot be completed because the environment or permissions are unavailable, record the exact limitation and stop.")
	return b.String()
}
