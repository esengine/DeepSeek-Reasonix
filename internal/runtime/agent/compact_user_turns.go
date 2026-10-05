package agent

import (
	"fmt"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// userTurnRetention is what one fold could and could not hold verbatim. A
// dropped turn is the loss compaction cannot undo — the workspace still holds
// the code a constraint governs, nothing holds the constraint — so it is
// counted and surfaced rather than absorbed.
type userTurnRetention struct {
	Kept          int
	Dropped       int
	DroppedTokens int
}

// keepUserTurns protects the user's own words from summarizer judgement: a
// constraint stated mid-session is unrecoverable once a digest drops it, while
// the work it governs stays re-derivable from the workspace. Unlike the keep
// policy it ignores policyStart — a bounded budget, not a fold horizon, is what
// stops it growing.
func (a *contextWindow) keepUserTurns(region []provider.Message, keep []bool) userTurnRetention {
	budget := a.keptUserTurnsBudget()
	var ret userTurnRetention
	// Oldest-first: the recent tail already covers the newest turns verbatim,
	// and an old turn has survived more folds than a new one.
	for i, m := range region {
		if m.Role != provider.RoleUser || m.LocalOnly || isCompactionSummary(m) {
			continue
		}
		if keep[i] {
			// Already held by the keep policy — a [[keep]] marker, which is the
			// documented way past the size budget below.
			ret.Kept++
			continue
		}
		// The remaining budget is the only gate. A separate per-turn ceiling
		// used to drop a long turn even when the budget could hold it, and
		// [[keep]] is the documented way to insist on a specific one.
		cost := fixedTokenEstimate(projectedUserTurnFloor(m))
		if cost > budget {
			ret.Dropped++
			ret.DroppedTokens += cost
			continue
		}
		keep[i] = true
		budget -= cost
		ret.Kept++
	}
	return ret
}

// keptUserTurnsBudget caps what user turns may spend of the checkpoint.
// Hoisting them unbounded is what made an earlier revision pad candidates past
// the acceptance ceiling, which fails compaction outright rather than degrading
// it — so a bound stays, but it scales with the window instead of stopping at a
// fixed token count, and the user can name their own.
func (a *contextWindow) keptUserTurnsBudget() int {
	if a.budgets.UserTurnKeepTokens > 0 {
		return a.budgets.UserTurnKeepTokens
	}
	window := a.effectiveContextWindow()
	if window <= 0 {
		return keptUserTurnsFloorTokens
	}
	// A fraction and nothing else: an absolute floor on top of it would let a
	// small window spend a large share of itself on retention, which pads the
	// candidate past the acceptance ceiling and fails the fold outright.
	return max(1, int(float64(window)*keptUserTurnsWindowFrac))
}

// noticeDroppedUserTurns reports the turns the budget could not hold. Without
// it the drop is invisible: the projection reads as complete, and the escape
// hatch is only useful to someone told it exists at the moment it is needed.
func (a *contextWindow) noticeDroppedUserTurns(ret userTurnRetention) {
	if ret.Dropped == 0 {
		return
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text: fmt.Sprintf("%s of yours %s too large to keep whole and now survive only through the summary. Prefix a turn with [[keep]] to hold it verbatim.",
			pluralTurns(ret.Dropped), wereOrWas(ret.Dropped)),
		Detail: fmt.Sprintf("compaction dropped %d user turn(s) (~%d tokens) past the retention budget of %d",
			ret.Dropped, ret.DroppedTokens, a.keptUserTurnsBudget())})
}

func pluralTurns(n int) string {
	if n == 1 {
		return "1 earlier message"
	}
	return fmt.Sprintf("%d earlier messages", n)
}

func wereOrWas(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}
