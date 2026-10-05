package agent

import (
	"fmt"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
)

// contextBudgetNoticeRatios are the fractions of the distance to the compaction
// trigger at which the host volunteers the remaining budget. Two rungs, not a
// gauge: the notice occupies the very context it reports on, so it earns a turn
// only where the model's next decision changes — once with room to redirect,
// once with room only to land what it already knows.
var contextBudgetNoticeRatios = [...]float64{0.75, 0.92}

// ContextBudget reports the room left before the next automatic fold, measured
// with the estimate the compaction thresholds themselves compare against so
// what the model is told and what the host acts on cannot drift apart.
func (a *Agent) ContextBudget() tool.ContextBudget {
	if a == nil {
		return unmeasuredContextBudget("no active agent session")
	}
	return a.window().contextBudget()
}

func (a *contextWindow) contextBudget() tool.ContextBudget {
	trigger := a.compactTrigger()
	window := a.effectiveContextWindow()
	if trigger <= 0 || window <= 0 {
		return unmeasuredContextBudget("the active provider declares no context window, so compaction is disabled")
	}
	used := a.contextUsedTokens()
	return tool.ContextBudget{
		Status:          "ok",
		TokensRemaining: max(0, trigger-used),
		TokensUsed:      used,
		CompactAt:       trigger,
		Window:          window,
	}
}

func unmeasuredContextBudget(reason string) tool.ContextBudget {
	return tool.ContextBudget{Status: "unmeasured", Reason: reason}
}

// budgetNoticeLatch remembers how far up the notice ladder this conversation has
// already been told, under the history build that makes it true. A fold drops
// usage and starts a new build, which re-arms every rung: the model about to be
// compacted and the model that just was need different things, and only the
// generation tells them apart.
type budgetNoticeLatch struct {
	rung       int
	generation ContextGeneration
}

// contextBudgetNotice returns the message to append before the next sampling
// call, or "" when the model has already been told what current pressure
// warrants. It advances the latch, so each rung fires once per fold.
func (a *contextWindow) contextBudgetNotice() string {
	if a == nil {
		return ""
	}
	if a.Session() == nil {
		return ""
	}
	notice, latch := advanceBudgetNotice(a.sess.win.budgetNotice, a.contextBudget(), a.contextGeneration())
	a.sess.win.budgetNotice = latch
	return notice
}

// advanceBudgetNotice decides what the current pressure warrants and returns the
// latch to store. Split from contextBudgetNotice so the edge-trigger rule is
// testable without a live session's token estimator.
func advanceBudgetNotice(latch budgetNoticeLatch, budget tool.ContextBudget, generation ContextGeneration) (string, budgetNoticeLatch) {
	if !budget.Known() {
		return "", latch
	}
	if latch.generation != generation {
		latch = budgetNoticeLatch{generation: generation}
	}
	rung := contextBudgetRung(budget)
	if rung <= latch.rung {
		return "", latch
	}
	latch.rung = rung
	return contextBudgetNoticeText(budget, rung), latch
}

// contextBudgetRung is the highest threshold the current usage has crossed;
// 0 means the conversation is still below the first one.
func contextBudgetRung(budget tool.ContextBudget) int {
	rung := 0
	for i, ratio := range contextBudgetNoticeRatios {
		if float64(budget.TokensUsed) >= float64(budget.CompactAt)*ratio {
			rung = i + 1
		}
	}
	return rung
}

// budgetContinuationClause is the half of the notice that is not a figure: the
// fold is routine and the task goes on across it. Without it a model reads
// "room remaining" as the end of the window and hands the work back.
const budgetContinuationClause = `Compaction is automatic and this same task continues after it. Keep working; do not stop, hand the work back, or suggest a new session because of this notice.`

func contextBudgetNoticeText(budget tool.ContextBudget, rung int) string {
	if rung >= len(contextBudgetNoticeRatios) {
		return fmt.Sprintf(`<context-budget>
About %d tokens remain before this conversation is automatically compacted (not before the %d-token window ends).
%s
Meanwhile write down what a summary would lose: the current result, the next step, any path or identifier held only in your replies.
</context-budget>`, budget.TokensRemaining, budget.Window, budgetContinuationClause)
	}
	return fmt.Sprintf(`<context-budget>
About %d tokens remain before this conversation is automatically compacted (%d used of a %d-token window; the fold triggers at %d).
%s
The user's turns stay verbatim; anything held only in your earlier replies — exact paths, line numbers, a half-finished plan — survives only if you restate it or put it in the todo list. Prefer scoped searches and ranged reads. Call context_budget for the current figure.
</context-budget>`, budget.TokensRemaining, budget.TokensUsed, budget.Window, budget.CompactAt, budgetContinuationClause)
}

// contextBudgetNoticeEvent is the user-facing record of the same event. The
// model gets the instructions; the user gets to see that it was told. Frontends
// localize by Code and read the figures from Detail; Text is the English
// fallback for sinks that know no code.
func contextBudgetNoticeEvent(budget tool.ContextBudget) event.Event {
	if !budget.Known() {
		return event.Event{Kind: event.Notice, Level: event.LevelWarn,
			Text: "Context budget unmeasured; the model was not notified."}
	}
	figures := event.ContextBudgetFigures{
		Percent:   int(float64(budget.TokensUsed) / float64(budget.CompactAt) * 100),
		Remaining: budget.TokensRemaining,
	}
	return event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   event.NoticeCodeContextBudget,
		Detail: figures.Encode(),
		Text: fmt.Sprintf("Context at %d%% of the compaction threshold — the model was told it has about %d tokens of room left.",
			figures.Percent, figures.Remaining),
	}
}
