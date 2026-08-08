package agent

import (
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

// applyBatchGuards runs both post-batch guards: the storm breaker (failure
// fixation) and the progress guard (zero-gain repetition).
func (a *Agent) applyBatchGuards(calls []provider.ToolCall, outcomes []toolOutcome, results []string, receiptMark int) {
	a.applyStormBreaker(calls, outcomes, results, receiptMark)
	a.applyProgressGuard(results, outcomes, receiptMark)
}

// resetTurnEvidence clears the ledger and the progress-guard state together.
func (a *Agent) resetTurnEvidence() {
	a.evidence.Reset()
	a.progress.reset()
}

// applyProgressGuard escalates when consecutive rounds stop producing new
// evidence. It rides the same channel as the storm breaker: guidance appended
// to the round's first tool result, a loop-guard notice for the frontend, and
// at the stop tier the loop-guard pass so final readiness stands down and the
// model can deliver its answer instead of being sent back for more receipts.
func (a *Agent) applyProgressGuard(results []string, outcomes []toolOutcome, receiptMark int) {
	if a.evidence == nil || len(results) == 0 || len(outcomes) == 0 {
		return
	}
	receipts := a.evidence.ReceiptsSince(receiptMark)
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
		return
	}
	streak := a.progress.observe(receipts)
	var guard, detail string
	warn := false
	// Fire only when a threshold is crossed: repeating the injected guidance
	// every round would inflate prompts (and can even tip compaction).
	switch streak {
	case progressStopStreak:
		guard = fmt.Sprintf(
			"[progress guard] %d tool rounds in a row produced no new evidence (no new files, results, or changes). Stop exploring: produce your final answer now, stating what was established and what remains unknown.",
			streak)
		detail = fmt.Sprintf("progress guard: %d zero-gain rounds — demanding a final answer", streak)
		warn = true
		a.armLoopGuardPass(receiptMark)
	case progressPivotStreak:
		guard = fmt.Sprintf(
			"[progress guard] still no new evidence after %d rounds. Change strategy now: take a different angle or tool, delegate a focused sub-task, or reduce the scope of what you are verifying.",
			streak)
		detail = fmt.Sprintf("progress guard: %d zero-gain rounds — forcing a strategy change", streak)
	case progressNudgeStreak:
		guard = fmt.Sprintf(
			"[progress guard] the last %d tool rounds repeated earlier reads or commands without new results. Narrow the investigation or adjust the plan before continuing.",
			streak)
		detail = fmt.Sprintf("progress guard: %d zero-gain rounds — nudging to narrow", streak)
	default:
		return
	}
	results[0] = outcomes[0].output + "\n\n" + guard
	level := event.LevelInfo
	if warn {
		level = event.LevelWarn
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: level, Code: event.NoticeCodeProgressGuard, Text: progressGuardNoticeText(), Detail: detail})
}

func progressGuardNoticeText() string {
	return "The assistant keeps repeating work without new evidence; asking it to change approach."
}

// armLoopGuardPass records that a loop guard fired this user turn.
// receiptMark is the evidence-ledger receipt count from just before the
// guarded batch ran, so a successful write or command receipt recorded after
// it counts as real progress and revokes the pass (see loopGuardAllowsFinal).
func (a *Agent) armLoopGuardPass(receiptMark int) {
	a.loopGuardArmed = true
	a.loopGuardReceiptMark = receiptMark
}

// loopGuardAllowsFinal reports whether final readiness should stand down: a
// guard fired this user turn and no successful write or command receipt has
// landed since. The missing receipts are exactly what the blocker prevents —
// demanding them would restart the loop the guard broke — while bookkeeping
// (ask, todo_write, complete_step) keeps the pass and real progress revokes it.
func (a *Agent) loopGuardAllowsFinal() bool {
	if a == nil || !a.loopGuardArmed {
		return false
	}
	if a.evidence == nil {
		return true
	}
	return !a.evidence.HasWriteOrCommandSince(a.loopGuardReceiptMark)
}
