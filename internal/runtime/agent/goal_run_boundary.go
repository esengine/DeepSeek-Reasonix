package agent

import (
	"errors"
	"fmt"

	"reasonix/internal/contract/provider"
)

// maxStepsPause is a resumable stop after a positive model-round budget.
type maxStepsPause struct {
	steps     int
	key       string
	hostOwned bool
}

func (e *maxStepsPause) Error() string {
	return fmt.Sprintf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set %s higher or to 0 for no limit", e.steps, e.key, e.key)
}

func IsToolLoopPause(err error) bool {
	var maxPause *maxStepsPause
	var budgetPause *taskBudgetPause
	return errors.As(err, &maxPause) || errors.As(err, &budgetPause)
}

// HostProgressSignatures exposes successful evidence identities to the Goal FSM.
func (a *Agent) HostProgressSignatures() []string {
	if a == nil || a.task.ledger == nil {
		return nil
	}
	return a.task.ledger.SuccessfulProgressSignaturesSince(0)
}

func (a *Agent) stopUnexecutedBoundaryCalls(state *turnRuntime, calls []provider.ToolCall, usage *provider.Usage) (error, bool) {
	switch {
	case state.graceRound:
		a.pairUnexecutedGraceCalls(calls, "blocked: the tool-call round budget is exhausted; no more tools will run in this turn")
		return a.gracePause(state), true
	default:
		return nil, false
	}
}

// resetTurnEvidence clears the ledger and the task budget together: a fresh
// ledger is what "a new task" means here, and a continuation keeps both.
func (a *Agent) resetTurnEvidence() {
	a.task.restartLedger()
}
