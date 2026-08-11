package agent

import (
	"fmt"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TaskBudget bounds one task on the axes its failures are reported in.
// Crossing either yields one tool-free summary and a resumable pause. Both
// axes ship off: stopping a task is the user's call, since only they know
// which model they are paying for and whether a long task is a runaway or the
// job they asked for.
type TaskBudget struct {
	Cost float64
	Wall time.Duration
}

// normalizeTaskBudget reads a negative value as unset, so a disabled axis and
// an unconfigured one behave identically.
func normalizeTaskBudget(b TaskBudget) TaskBudget {
	if b.Cost < 0 {
		b.Cost = 0
	}
	if b.Wall < 0 {
		b.Wall = 0
	}
	return b
}

// runBudget accumulates what a turn has actually spent. Rounds are a poor proxy
// for it: the same hundred of them cost minutes or hours depending on what each
// one read and how long the model thought, and the failures worth stopping are
// reported in hours and tokens, never in rounds.
type runBudget struct {
	started       time.Time
	rounds        int
	requests      int
	promptTokens  int
	outputTokens  int
	cost          float64
	pricedRounds  int
	unpricedTurns bool
	// limit is configuration, not accumulation: it survives the reset that
	// starts a new task.
	limit TaskBudget
}

// observe folds one round's provider usage into the turn's running total.
// A round whose usage never arrived still counts as a round, so the axis never
// reads cheaper than the turn actually was.
func (b *runBudget) observe(usage *provider.Usage, pricing *provider.Pricing) {
	b.rounds++
	if usage == nil {
		return
	}
	b.requests += usageRequestCount(usage)
	b.promptTokens += usage.PromptTokens
	b.outputTokens += usage.CompletionTokens
	if pricing == nil {
		b.unpricedTurns = true
		return
	}
	b.cost += pricing.Cost(usage)
	b.pricedRounds++
}

func (b *runBudget) elapsed() time.Duration {
	if b.started.IsZero() {
		return 0
	}
	return time.Since(b.started)
}

// totals is the shadow reading for one scope: counts and money, never content.
func (b *runBudget) totals() event.RunBudgetTotals {
	return event.RunBudgetTotals{
		Rounds:       b.rounds,
		Requests:     b.requests,
		PromptTokens: b.promptTokens,
		OutputTokens: b.outputTokens,
		Cost:         b.cost,
		Priced:       !b.unpricedTurns && b.pricedRounds > 0,
		ElapsedMs:    b.elapsed().Milliseconds(),
	}
}

// exceeded names the first axis the task has spent past, or "" while inside
// the budget. Cost only counts when the turn was actually priced: an unpriced
// model reads as free, and a free reading must never look like a crossing.
func (b *runBudget) exceeded(limit TaskBudget) (axis, detail string) {
	if limit.Cost > 0 && b.pricedRounds > 0 && !b.unpricedTurns && b.cost >= limit.Cost {
		return "cost", fmt.Sprintf("task spend %.4f reached the %.4f budget", b.cost, limit.Cost)
	}
	if limit.Wall > 0 {
		if elapsed := b.elapsed(); elapsed >= limit.Wall {
			return "time", fmt.Sprintf("task ran %s, past the %s budget",
				elapsed.Round(time.Second), limit.Wall)
		}
	}
	return "", ""
}

// observeRunBudget folds a round into both scopes and reports them.
func (a *Agent) observeRunBudget(state *runLoopState, usage *provider.Usage) {
	if state == nil {
		return
	}
	state.budget.observe(usage, a.pricing)
	if a.taskBudget.started.IsZero() {
		a.taskBudget.started = state.budget.started
	}
	a.taskBudget.observe(usage, a.pricing)
	currency := ""
	if a.pricing != nil {
		currency = a.pricing.Symbol()
	}
	event.RecordRunBudget(a.sink, event.RunBudgetSample{
		Turn:     state.budget.totals(),
		Task:     a.taskBudget.totals(),
		Currency: currency,
	})
}
