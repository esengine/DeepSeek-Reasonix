package agent

import "reasonix/internal/evidence"

// taskRuntime is the host state shared by every Agent.Run continuing one
// delivery scope: one ledger, one bill, one set of failure budgets. Its
// lifetime sits between the session and the turn — SetSession replaces it
// wholesale, beginRunTurn restarts it when the scope changes, and state valid
// for exactly one Run lives in perTurnState instead.
type taskRuntime struct {
	scopeID    string
	checkpoint evidence.DeliveryCheckpoint
	ledger     *evidence.Ledger
	outcome    *evidence.OutcomeTracker
	budget     runBudget
	ebm        ebmState
	governor   governorState
	// repeatFailures outlives a Run: re-reading a file and resending the same
	// stale anchor is still zero progress, so prepareScope — not the ledger
	// restart — decides what survives a scope-stable continuation.
	repeatFailures map[string]repeatFailureRecord
	repeatScope    string
}

// restartLedger begins a new task's accounting. It is written as one assignment
// so a field added to taskRuntime resets by default; the four fields carried
// forward are named because each answers to its own condition in beginRunTurn.
func (t *taskRuntime) restartLedger() {
	*t = taskRuntime{
		scopeID:        t.scopeID,
		checkpoint:     t.checkpoint,
		repeatFailures: t.repeatFailures,
		repeatScope:    t.repeatScope,
		ledger:         t.ledger,
		outcome:        evidence.NewOutcomeTracker(),
		budget:         runBudget{limit: t.budget.limit},
	}
	t.ledger.Reset()
}

// prepareScope reconciles the repeat-failure records against the scope this Run
// belongs to. A scope-stable continuation keeps only the records whose anchor
// still needs re-checking; anything else starts from empty.
func (t *taskRuntime) prepareScope(scoped bool, scopeID string) {
	if !scoped || t.repeatScope != scopeID {
		t.repeatFailures = nil
	} else {
		for sig, failure := range t.repeatFailures {
			if !failure.stateRecheck {
				delete(t.repeatFailures, sig)
			}
		}
	}
	if scoped {
		t.repeatScope = scopeID
	} else {
		t.repeatScope = ""
	}
}
