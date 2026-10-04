package agent

import (
	"reasonix/internal/runtime/contract"
	"sync/atomic"

	"reasonix/internal/safety/evidence"
)

// taskRuntime is the host state shared by every Agent.Run continuing one
// delivery scope: one ledger, one bill, one set of failure budgets. Its
// lifetime sits between the session and the turn — SetSession replaces it
// wholesale, beginRunTurn restarts it when the scope changes, and state valid
// for exactly one Run lives in perTurnState instead.
type taskRuntime struct {
	scopeID    string
	checkpoint evidence.DeliveryCheckpoint
	// baselineCriteria names what each rewritten file's criteria said first,
	// keyed by path. The first capture of a path wins.
	baselineCriteria map[string]evidence.TestCriterion
	// contract caches the revision checkpoint.Contract names; contractRecord is
	// the record it was read from, so a checkpoint naming another invalidates it.
	contract       *contract.Contract
	contractRecord string
	ledger         *evidence.Ledger
	outcome        *evidence.OutcomeTracker
	budget         runBudget
	// witness holds, per changed path, the lines a later output has to carry to
	// have shown that change. It is working state, not evidence: the verdict
	// lands on the receipt, so the ledger never holds file content.
	witness map[string][]string
	// todoRevs counts what the task list did, on three axes because they answer
	// different questions and one number for all three reads a rewrite as work.
	todoRevs todoRevisions
	// overScanLimit remembers that the effect walk cannot finish here, so later
	// calls do not pay to be told again. A pointer because taskRuntime is
	// assigned wholesale, which an atomic cannot be.
	overScanLimit *atomic.Bool
	// criteriaEpoch is the workspace state the broad-scope criteria capture last
	// ran against, plus one so zero reads as never. A pointer for the same
	// reason overScanLimit is one.
	criteriaEpoch  *atomic.Uint64
	workspaceProse *workspaceProseCache
}

func newTaskRuntime(limit TaskBudget) taskRuntime {
	return taskRuntime{
		ledger:         evidence.NewLedger(),
		workspaceProse: new(workspaceProseCache),
		budget:         runBudget{limit: normalizeTaskBudget(limit)},
	}
}

// Nil-safe: a zero taskRuntime has no memo, which costs the walk it would have
// skipped and answers exactly as before.
func (t *taskRuntime) workspaceOverScanLimit() bool {
	return t != nil && t.overScanLimit != nil && t.overScanLimit.Load()
}

func (t *taskRuntime) noteWorkspaceOverScanLimit() {
	if t != nil && t.overScanLimit != nil {
		t.overScanLimit.Store(true)
	}
}

// criteriaHeldAt reports that the capture already ran against this state, so
// walking again would re-derive an answer nothing has moved.
func (t *taskRuntime) criteriaHeldAt(epoch uint64) bool {
	return t != nil && t.criteriaEpoch != nil && t.criteriaEpoch.Load() == epoch+1
}

func (t *taskRuntime) noteCriteriaCaptured(epoch uint64) {
	if t != nil && t.criteriaEpoch != nil {
		t.criteriaEpoch.Store(epoch + 1)
	}
}

// todoRevisions separates writing the plan from changing it and from executing
// it. progress counts only host-observed execution movement, so a turn cannot
// renew it by restating its own list.
type todoRevisions struct {
	content  int
	plan     int
	progress int
}

// restartLedger begins a new task's accounting. It is written as one assignment
// so a field added to taskRuntime resets by default; the fields carried forward
// are named because each answers to its own condition in beginRunTurn.
func (t *taskRuntime) restartLedger() {
	*t = taskRuntime{
		scopeID:        t.scopeID,
		checkpoint:     t.checkpoint,
		ledger:         t.ledger,
		outcome:        evidence.NewOutcomeTracker(),
		budget:         runBudget{limit: t.budget.limit},
		overScanLimit:  new(atomic.Bool),
		criteriaEpoch:  new(atomic.Uint64),
		workspaceProse: new(workspaceProseCache),
	}
	// A new task is accepted under a new contract; a resumed one is not
	// restarted, so the revision its checkpoint names stays in force.
	t.checkpoint.Contract = ""
	t.ledger.Reset()
}
