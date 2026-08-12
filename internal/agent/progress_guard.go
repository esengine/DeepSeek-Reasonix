package agent

import (
	"context"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// The no-progress ladder is adaptive, not a fixed round count: rounds are
// judged by evidence gain (new reads, new results, mutations) and only
// consecutive zero-gain rounds escalate — nudge, then pivot, then stop.
const (
	progressNudgeStreak = 2
	progressPivotStreak = 4
	progressStopStreak  = 6
)

// progressGuard tracks consecutive tool rounds whose receipts produced no new
// evidence. State lives per user turn, alongside the ledger it observes.
type progressGuard struct {
	tracker *evidence.ProgressTracker
	streak  int
}

func (g *progressGuard) reset() {
	g.tracker = evidence.NewProgressTracker()
	g.streak = 0
}

// observe folds one round's receipts and returns the current zero-gain streak.
func (g *progressGuard) observe(receipts []Receipt) int {
	if g.tracker == nil {
		g.reset()
	}
	if len(receipts) == 0 {
		return g.streak
	}
	if g.tracker.ScoreRound(receipts) > 0 {
		g.streak = 0
	} else {
		g.streak++
	}
	return g.streak
}

// Receipt aliases the evidence receipt for the guard's signature.
type Receipt = evidence.Receipt

// applyBatchGuards collects this round's signals — storm breaker (failure
// fixation), progress guard (zero-gain repetition), evidence nudge — and lets
// the arbiter deliver them as one tail. The shadow trackers observe the same
// receipts without influencing any verdict.
func (a *Agent) applyBatchGuards(ctx context.Context, cancelled bool, calls []provider.ToolCall, outcomes []toolOutcome, results []string, receiptMark int) {
	if cancelled {
		return
	}
	storm := a.applyStormBreaker(calls, outcomes, receiptMark)
	_, goalScoped := DeliveryExecutionScopeFromContext(ctx)
	progress := a.applyProgressGuard(outcomes, receiptMark, goalScoped)
	shadow := a.observeOutcomeShadow(receiptMark, outcomes)
	a.applyInterventions(results, outcomes, storm, progress, shadow)
	a.observeDelegationAdmission(calls)
}

// resetTurnEvidence clears the ledger and both progress scorers together. The
// task budget resets with them: a fresh ledger is what "a new task" means here,
// and a continuation keeps both.
func (a *Agent) resetTurnEvidence() {
	a.task.restartLedger()
	a.turn.progress.reset()
	a.turn.stormSig, a.turn.stormCount, a.turn.blockedTurnStreak = "", 0, 0
}

// observeOutcomeShadow scores the round's receipts through the shadow outcome
// tracker, lets the EBM policy stamp (and under its arm, act on) the sample,
// then records it. Unlike the guards it observes every round.
func (a *Agent) observeOutcomeShadow(receiptMark int, outcomes []toolOutcome) intervention {
	if a.task.ledger == nil {
		return intervention{}
	}
	if a.task.outcome == nil {
		a.task.outcome = evidence.NewOutcomeTracker()
	}
	sample := a.task.outcome.ScoreRound(a.task.ledger.ReceiptsSince(receiptMark))
	iv := a.applyEBM(&sample, outcomes)
	a.applyGovernor(&sample)
	a.armGovernorCapture(sample)
	event.RecordOutcomeProgress(a.sink, sample)
	a.observeContractRound()
	return iv
}

// applyProgressGuard escalates when consecutive rounds stop producing new
// evidence. At the stop tier it also arms the loop-guard pass so final
// readiness stands down and the model can deliver its answer instead of being
// sent back for more receipts.
func (a *Agent) applyProgressGuard(outcomes []toolOutcome, receiptMark int, goalScoped bool) intervention {
	if a.task.ledger == nil || len(outcomes) == 0 {
		return intervention{}
	}
	receipts := a.task.ledger.ReceiptsSince(receiptMark)
	// Rounds where nothing succeeded are the storm breaker's jurisdiction
	// (same-failure fixation); this guard owns the storm-blind case — rounds
	// that keep SUCCEEDING without producing anything new.
	anySuccess := false
	for _, r := range receipts {
		if r.Success {
			anySuccess = true
			break
		}
	}
	if !anySuccess {
		return intervention{}
	}
	streak := a.turn.progress.observe(receipts)
	var guard, detail string
	tier := verdictAdvise
	warn := false
	// Fire only when a threshold is crossed: repeating the injected guidance
	// every round would inflate prompts (and can even tip compaction).
	switch streak {
	case progressStopStreak:
		if goalScoped {
			guard = fmt.Sprintf(
				"[progress guard] %d tool rounds in a row produced no new evidence. Re-plan and continue: shrink the current step, switch tools or approach, delegate a focused sub-task, or report a real external/user blocker through update_goal. Do not repeat the same calls.",
				streak)
			detail = fmt.Sprintf("progress guard: %d zero-gain rounds — forcing a Goal re-plan", streak)
			tier = verdictRedirect
			// Start a fresh intervention epoch. The evidence tracker stays intact,
			// so repeated work remains visible while a changed strategy can recover.
			a.turn.progress.streak = 0
		} else {
			guard = fmt.Sprintf(
				"[progress guard] %d tool rounds in a row produced no new evidence (no new files, results, or changes). Stop exploring: produce your final answer now, stating what was established and what remains unknown.",
				streak)
			detail = fmt.Sprintf("progress guard: %d zero-gain rounds — demanding a final answer", streak)
			tier = verdictLand
			warn = true
			a.armLoopGuardPass(receiptMark)
		}
	case progressPivotStreak:
		guard = fmt.Sprintf(
			"[progress guard] still no new evidence after %d rounds. Change strategy now: take a different angle or tool, delegate a focused sub-task, or reduce the scope of what you are verifying.",
			streak)
		detail = fmt.Sprintf("progress guard: %d zero-gain rounds — forcing a strategy change", streak)
		tier = verdictRedirect
	case progressNudgeStreak:
		guard = fmt.Sprintf(
			"[progress guard] the last %d tool rounds repeated earlier reads or commands without new results. Narrow the investigation or adjust the plan before continuing.",
			streak)
		detail = fmt.Sprintf("progress guard: %d zero-gain rounds — nudging to narrow", streak)
	default:
		return intervention{}
	}
	level := event.LevelInfo
	if warn {
		level = event.LevelWarn
	}
	return intervention{
		verdict:  tier,
		guidance: guard,
		notice:   noticeFor(event.NoticeCodeProgressGuard, level, progressGuardNoticeText(), detail),
	}
}

func progressGuardNoticeText() string {
	return "The assistant keeps repeating work without new evidence; asking it to change approach."
}

// armLoopGuardPass records that a loop guard fired this user turn.
// receiptMark is the evidence-ledger receipt count from just before the
// guarded batch ran, so a successful write or command receipt recorded after
// it counts as real progress and revokes the pass (see loopGuardAllowsFinal).
func (a *Agent) armLoopGuardPass(receiptMark int) {
	a.turn.loopGuardArmed = true
	a.turn.loopGuardReceiptMark = receiptMark
}

// loopGuardAllowsFinal reports whether final readiness should stand down: a
// guard fired this user turn and no successful write or command receipt has
// landed since. The missing receipts are exactly what the blocker prevents —
// demanding them would restart the loop the guard broke — while bookkeeping
// (ask, todo_write, complete_step) keeps the pass and real progress revokes it.
func (a *Agent) loopGuardAllowsFinal() bool {
	if a == nil || !a.turn.loopGuardArmed {
		return false
	}
	if a.task.ledger == nil {
		return true
	}
	return !a.task.ledger.HasWriteOrCommandSince(a.turn.loopGuardReceiptMark)
}
