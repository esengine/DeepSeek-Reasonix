package agent

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TaskBudget bounds one task on the axes its failures are reported in, and
// every axis ships off: stopping a task is the user's call. Tokens is the one
// that generalizes — a slow expensive loop accumulates them and so does a fast
// empty one, where wall clock catches only the first and money is not portable
// across models.
type TaskBudget struct {
	Cost   float64
	Wall   time.Duration
	Tokens int
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
	if b.Tokens < 0 {
		b.Tokens = 0
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
	var quote *billing.CostQuote
	if usage != nil && pricing != nil {
		quote = event.EnsureCostQuote(event.Event{Kind: event.Usage, Usage: usage, Pricing: pricing}, nil)
	}
	b.observeQuote(usage, quote)
}

func (b *runBudget) observeQuote(usage *provider.Usage, quote *billing.CostQuote) {
	b.rounds++
	if usage == nil {
		return
	}
	b.requests += usageRequestCount(usage)
	b.promptTokens += usage.PromptTokens
	b.outputTokens += usage.CompletionTokens
	if quote == nil || !quote.CostComplete || quote.Original.Currency == "" {
		b.unpricedTurns = true
		return
	}
	b.cost += quote.Original.Float64()
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
	if limit.Tokens > 0 {
		if used := b.promptTokens + b.outputTokens; used >= limit.Tokens {
			return "token", fmt.Sprintf("task used %d tokens, reaching the %d budget", used, limit.Tokens)
		}
	}
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

// taskBudgetLimit resolves this turn's bound: a host-injected budget wins over
// the configured one, which is how an unattended loop gets a ceiling while
// ordinary chat keeps none.
func (a *Agent) taskBudgetLimit(ctx context.Context) TaskBudget {
	if b, ok := taskBudgetFromContext(ctx); ok {
		return b
	}
	return a.task.budget.limit
}

// ResetTaskBudget starts a fresh user-approved spend slice without touching
// Delivery evidence or the persisted Goal usage totals. Callers use this only
// after a resumable explicit-budget pause, while no Agent Run is active.
func (a *Agent) ResetTaskBudget() {
	a.task.budget = runBudget{limit: a.task.budget.limit}
}

// observeRunBudget folds a round into both scopes and reports them.
func (a *Agent) observeRunBudget(state *turnRuntime, usage *provider.Usage, quotes ...*billing.CostQuote) {
	if state == nil {
		return
	}
	var quote *billing.CostQuote
	if len(quotes) > 0 {
		quote = quotes[0]
	} else if usage != nil && a.svc.pricing != nil {
		e := event.Event{Kind: event.Usage, ModelRef: a.modelRef, Usage: usage, Pricing: a.svc.pricing, UsageSource: a.usageSource}
		quote = event.EnsureCostQuote(e, a.svc.quoteContext)
	}
	state.budget.observeQuote(usage, quote)
	if a.task.budget.started.IsZero() {
		a.task.budget.started = state.budget.started
	}
	a.task.budget.observeQuote(usage, quote)
	currency := ""
	if quote != nil {
		currency = billing.CurrencySymbol(quote.Original.Currency)
	}
	event.RecordRunBudget(a.svc.sink, event.RunBudgetSample{
		Turn:     state.budget.totals(),
		Task:     a.task.budget.totals(),
		Currency: currency,
	})
}

type taskBudgetContextKey struct{}

// WithTaskBudget overrides a run's task budget for one turn. The agent serving
// an unattended loop and the one serving chat are the same instance, so the
// bound is a property of the turn, not of construction.
func WithTaskBudget(ctx context.Context, b TaskBudget) context.Context {
	return context.WithValue(ctx, taskBudgetContextKey{}, normalizeTaskBudget(b))
}

func taskBudgetFromContext(ctx context.Context) (TaskBudget, bool) {
	if ctx == nil {
		return TaskBudget{}, false
	}
	b, ok := ctx.Value(taskBudgetContextKey{}).(TaskBudget)
	return b, ok
}
