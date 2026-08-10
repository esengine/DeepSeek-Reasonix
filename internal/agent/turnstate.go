package agent

import "reasonix/internal/completion"

// perTurnState is the host state valid for exactly one Agent.Run, embedded in
// Agent so field access stays flat while the lifetime is explicit. beginRunTurn
// zeroes it in a single assignment before computing the new turn's values; a
// field added here can never be forgotten in the reset. Anything that must
// survive turns (delivery checkpoint/scope, failure budgets, storm counters)
// stays directly on Agent.
type perTurnState struct {
	// turnInput is this run's task text. The contract is rebuilt from it and
	// the ledger whenever a live view is needed, so one replay serves both the
	// per-round observation and the end-of-turn record.
	turnInput string
	// completion is the report built as the turn ends; the host reads it while
	// emitting TurnDone, before the next turn resets this state.
	completion *completion.Report
	// Delivery expectations classified from the task text (see taskintent).
	// deliveryCriteriaEstablished may inherit an unfinished canonical task
	// list on continuation, but the flag itself is recomputed every turn.
	deliveryCriteriaEstablished bool
	deliveryTaskExpected        bool
	deliveryMutationExpected    bool
	deliveryPersistentExpected  bool
	deliveryScopeActive         bool
	// readinessRecovered marks a run that started with evidence preserved from
	// (or a pending recovery of) a prior readiness failure, so the final
	// allowed audit can report Recovered=true.
	readinessRecovered bool

	// recoveryTaskSummary is the bounded task text for this Agent.Run. It lets
	// a shared recovery gate review sub-agent mutations against the child
	// task, rather than the root controller transcript.
	recoveryTaskSummary string

	// blockedTurnStreak counts consecutive turns in which every tool call was
	// blocked by the host (permission, plan mode, hook, or loop guard).
	// stormSig catches a model fixated on one call shape; this catches a model
	// rotating between blocked shapes — alternating tools, reordering a batch,
	// or blockers whose text varies per attempt — which is zero progress all
	// the same. Reset by any turn containing a non-blocked outcome and at the
	// start of each user turn. See applyStormBreaker.
	blockedTurnStreak int

	// loopGuardArmed / loopGuardReceiptMark let final readiness stand down
	// after a loop guard fired this user turn: once the host has told the model
	// to stop retrying and report the blocker, demanding the receipts that the
	// blocker prevents would restart the loop the guard just broke. The mark is
	// the evidence-ledger receipt count from just before the guarded batch, so
	// real progress — a successful write or command receipt landing after it —
	// revokes the pass, while the bookkeeping the guard itself recommends
	// (ask, todo_write, complete_step) keeps it. Host state, not message text:
	// tool output that merely quotes "[loop guard]" must not unlock readiness.
	// See loopGuardAllowsFinal.
	loopGuardArmed       bool
	loopGuardReceiptMark int

	// repeatSuccessCounts tracks write-like tool calls that have already
	// succeeded in this user turn. This catches the complementary loop shape to
	// stormSig: a model keeps doing the same successful write, so there is no
	// error for the failure-only storm breaker to see.
	repeatSuccessCounts map[string]int
}
