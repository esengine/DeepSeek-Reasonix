package agent

import (
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/provider"
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
func (a *Agent) keepUserTurns(region []provider.Message, keep []bool) userTurnRetention {
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
		cost := fixedTokenEstimate(m)
		if cost > maxKeptUserTurnTokens || cost > budget {
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

// keptUserTurnsBudget caps what user turns may spend of the checkpoint. Hoisting
// them unbounded is what made an earlier revision pad candidates past the
// acceptance ceiling, which fails compaction outright rather than degrading it.
func (a *Agent) keptUserTurnsBudget() int {
	if a.contextWindow <= 0 {
		return keptUserTurnsBudgetTokens
	}
	return min(keptUserTurnsBudgetTokens, int(float64(a.contextWindow)*keptUserTurnsWindowFrac))
}

// noticeDroppedUserTurns reports the turns the budget could not hold. Without
// it the drop is invisible: the projection reads as complete, and the escape
// hatch is only useful to someone told it exists at the moment it is needed.
func (a *Agent) noticeDroppedUserTurns(ret userTurnRetention) {
	if ret.Dropped == 0 {
		return
	}
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
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
