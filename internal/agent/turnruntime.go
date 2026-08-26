package agent

import (
	"reasonix/internal/completion"
	"reasonix/internal/runtimepolicy"
)

// turnRuntime is the host state for exactly one Agent.Run. beginRunTurn builds
// it in a single assignment, so a field added here starts the next turn zeroed.
// State an external caller arms before a Run lives in pendingTurn; state that
// outlives the Run lives in taskRuntime or sessionRuntime.
type turnRuntime struct {
	runMaxSteps    int
	runMaxStepsKey string

	emptyFinalBlocks   int
	handoffNudges      int
	usedAnyTool        bool
	contextToolRepairs int
	protocolRepairs    int
	finishCalls        int
	pendingFinalAnswer bool
	finishOutcome      FinishOutcome
	graceRound         bool
	recoveryGraceRound bool

	todoProgress         int
	trackingTodoProgress bool
	todoStallRounds      int
	seenTodoProgress     map[string]struct{}

	executorHandoff bool
	input           string
	workDurationMs  func() int64

	// budget is the turn's spend axis: tokens, money, wall clock.
	budget runBudget
	// landCause records why the grace round was armed, so the pause the Run
	// ends with names the axis that actually stopped it.
	landCause landCause

	// turnInput is this run's task text. The contract is rebuilt from it and
	// the ledger whenever a live view is needed, so one replay serves both the
	// per-round observation and the end-of-turn record.
	turnInput string
	// completion is the report built as the turn ends; the host reads it while
	// emitting TurnDone, before the next turn resets this state.
	completion *completion.Report
	// deliveryCriteriaEstablished may inherit an unfinished canonical task
	// list on continuation, but the flag itself is recomputed every turn.
	deliveryCriteriaEstablished bool
	deliveryScopeActive         bool
	// readinessRecovered marks a run that started with evidence preserved from
	// (or a pending recovery of) a prior readiness failure, so the final
	// allowed audit can report Recovered=true.
	readinessRecovered bool

	// recoveryTaskSummary is the bounded task text for this Agent.Run. It lets
	// a shared recovery gate review sub-agent mutations against the child
	// task, rather than the root controller transcript.
	recoveryTaskSummary string

	// blockedTurnStreak counts consecutive rounds the host blocked outright.
	// stormSig catches fixation on one call shape; this catches rotation
	// between blocked shapes, which is zero progress all the same.
	blockedTurnStreak int

	// loopGuardArmed stands final readiness down after a loop guard fired:
	// demanding receipts the blocker prevents would restart the loop. The mark
	// is the pre-batch ledger count, so later progress revokes the pass.
	loopGuardArmed       bool
	loopGuardReceiptMark int

	// repeatSuccessCounts catches the shape stormSig cannot see: the same write
	// succeeding over and over leaves no error for a failure-only breaker.
	repeatSuccessCounts map[string]int

	// constraints and engine are frozen at the start of the Run.
	constraints runtimepolicy.Constraints
	engine      *runtimepolicy.Engine

	// reviewWarnings are warn-level findings to surface in the final summary.
	reviewWarnings []string

	// stormSig keys on (tool, error/blocker), NOT (tool, args): a stuck model
	// reworks arguments cosmetically while the host returns the same refusal,
	// so keying on args misses the loop entirely. See applyStormBreaker.
	stormSig   string
	stormCount int

	// progress escalates adaptively on consecutive zero-evidence-gain rounds;
	// see progress_guard.go.
	progress progressGuard

	// lastReasoning is the previous executor round's reasoning-token spend,
	// read by the governor trigger (live policy and fork capture alike).
	lastReasoning int
}

// TurnFinishOutcome is read while emitting TurnDone, before the next admission
// can reset the per-run state.
func (a *Agent) TurnFinishOutcome() FinishOutcome {
	if a == nil {
		return ""
	}
	return a.turn.finishOutcome
}

// pendingTurn is what someone outside the Run arms for the next one: a
// sub-agent spawner, the turn that just failed readiness, or the fork capture.
// It is deliberately not in turnRuntime — beginRunTurn builds that fresh, and
// state armed before it exists would be wiped by the same assignment that makes
// turnRuntime safe.
type pendingTurn struct {
	// preserveEvidence makes the next Run keep the turn evidence ledger instead
	// of resetting it, so a review_report completion nudge can cite the read
	// receipts the subagent already earned. Consumed by that Run.
	preserveEvidence bool
	// finalReadinessRecovery is armed after final readiness fails. An explicit
	// host action preserves receipts once; an ordinary turn resets evidence.
	finalReadinessRecovery bool
	// finalReadinessRecoveryPrepared prevents the durable marker fallback from
	// being consumed twice before the prepared Run starts.
	finalReadinessRecoveryPrepared bool
	// forkRestore, when armed, swaps the frozen fork-bundle conversation in
	// right after beginRunTurn — the counterfactual-continuation seam.
	forkRestore func(*turnRuntime)
}
