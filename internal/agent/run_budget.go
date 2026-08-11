package agent

import (
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

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

// sample is the per-round shadow reading: counts and money, never content.
func (b *runBudget) sample(currency string) event.RunBudgetSample {
	return event.RunBudgetSample{
		Rounds:       b.rounds,
		Requests:     b.requests,
		PromptTokens: b.promptTokens,
		OutputTokens: b.outputTokens,
		Cost:         b.cost,
		Currency:     currency,
		Priced:       !b.unpricedTurns && b.pricedRounds > 0,
		ElapsedMs:    b.elapsed().Milliseconds(),
	}
}

// observeRunBudget records the turn's spend after a round and reports it to
// sinks that opt in. Shadow only: nothing reads the verdict yet, and no
// threshold exists to read it against until the recorded distribution says
// where one belongs.
func (a *Agent) observeRunBudget(state *runLoopState, usage *provider.Usage) {
	if state == nil {
		return
	}
	state.budget.observe(usage, a.pricing)
	currency := ""
	if a.pricing != nil {
		currency = a.pricing.Symbol()
	}
	event.RecordRunBudget(a.sink, state.budget.sample(currency))
}
