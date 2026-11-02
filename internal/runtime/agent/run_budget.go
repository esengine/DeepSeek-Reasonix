package agent

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
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
	started      time.Time
	rounds       int
	requests     int
	promptTokens int
	outputTokens int
	tokens       uint64
	cost         runCost
	// limit is configuration, not accumulation: it survives the reset that
	// starts a new task.
	limit      TaskBudget
	rootTokens *rootTokenBudget
	rootOrigin rootBudgetOrigin
}

// observe folds one round's provider usage into the turn's running total.
// A round whose usage never arrived still counts as a round, so the axis never
// reads cheaper than the turn actually was.
func (b *runBudget) observe(usage *provider.Usage, quote *pricing.CostQuote) {
	b.rounds++
	b.tokens = addBudgetTokens(b.tokens, budgetUsageTokens(usage))
	b.cost.observe(usage, quote)
	if usage == nil {
		return
	}
	b.requests += usageRequestCount(usage)
	b.promptTokens += usage.PromptTokens
	b.outputTokens += usage.CompletionTokens
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
		Cost:         b.cost.amount().Float64(),
		Priced:       b.cost.complete(),
		ElapsedMs:    b.elapsed().Milliseconds(),
	}
}

// exceeded names the first axis the task has spent past, or "" while inside
// the budget. Known priced spend is a lower bound even when other rounds lack
// usage or prices; unknown spend cannot erase a proven crossing.
func (b *runBudget) exceeded(limit TaskBudget) (axis, detail string) {
	if limit.Tokens > 0 {
		if used := b.tokens; used >= uint64(limit.Tokens) {
			return "token", fmt.Sprintf("task used %d tokens, reaching the %d budget", used, limit.Tokens)
		}
	}
	if limit.Cost > 0 && b.cost.pricedRounds > 0 && b.cost.amount() >= max(pricing.Amount(1), pricing.NewAmountFromFloat(limit.Cost)) {
		return "cost", fmt.Sprintf("task spend %.4f reached the %.4f budget", b.cost.amount().Float64(), limit.Cost)
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
func (a *Agent) observeRunBudget(state *turnRuntime, usage *provider.Usage, quote *pricing.CostQuote) {
	if state == nil {
		return
	}
	state.budget.rootTokens.observe(usage)
	state.budget.observe(usage, quote)
	if a.task.budget.started.IsZero() {
		a.task.budget.started = state.budget.started
	}
	a.task.budget.observe(usage, quote)
	currency := ""
	if a.svc.pricing != nil {
		currency = a.svc.pricing.Symbol()
	}
	if state.budget.cost.currency != "" {
		currency = pricing.CurrencySymbol(state.budget.cost.currency)
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
