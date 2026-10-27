package agent

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
	"time"
)

func budgetQuote(prompt, output int, rates *provider.Pricing) (pricing.Amount, string, bool) {
	quote := event.EnsureCostQuote(event.Event{Kind: event.Usage, Usage: &provider.Usage{PromptTokens: prompt, CacheMissTokens: prompt, CompletionTokens: output}, Pricing: rates}, nil)
	if quote == nil || !quote.CostComplete {
		return 0, "", false
	}
	amount, err := pricing.ParseAmount(quote.Original.Amount)
	return amount, quote.Original.Currency, err == nil
}

func (b *sharedBudget) limited() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.limit.Tokens > 0 || b.limit.Cost > 0 || b.limit.Wall > 0
}

func (b *sharedBudget) reserve(ctx context.Context, prompt, maxOutput int, rates *provider.Pricing) (string, int, error) {
	if prompt < 0 || maxOutput < 0 || prompt > math.MaxInt-maxOutput {
		return "", 0, fmt.Errorf("invalid task budget request size")
	}
	id := rand.Text()
	output := max(1, maxOutput)
	err := b.transact(ctx, func() error {
		if b.limit.Wall > 0 && time.Since(b.started) >= b.limit.Wall {
			return &taskBudgetPause{axis: "time", detail: "the persisted task deadline has elapsed"}
		}
		tokens, cost := b.tokens, b.cost[b.currency]
		for _, hold := range b.reserved {
			tokens = budgetTokenSum(tokens, hold.Tokens)
			if hold.Currency == b.currency {
				cost = cost.Add(hold.Cost)
			}
		}
		if b.limit.Tokens > 0 {
			remaining := max(0, b.limit.Tokens-tokens)
			output = min(output, remaining-prompt)
			if output < 1 {
				return &taskBudgetPause{axis: "token", detail: "remaining allowance cannot reserve the next prompt and response"}
			}
		}
		amount, currency, priced := budgetQuote(prompt, output, rates)
		if b.limit.Cost > 0 {
			if !priced || currency != b.currency || currency == "" {
				return &taskBudgetPause{axis: "cost", detail: "the next request has no comparable price; cost reservation is unavailable"}
			}
			remaining := pricing.NewAmountFromFloat(b.limit.Cost) - cost
			low, high := 0, output
			for low < high {
				mid := low + (high-low)/2 + (high-low)%2
				quote, _, _ := budgetQuote(prompt, mid, rates)
				if quote <= remaining {
					low = mid
				} else {
					high = mid - 1
				}
			}
			output = low
			if output < 1 {
				return &taskBudgetPause{axis: "cost", detail: "remaining allowance cannot reserve the next request"}
			}
			amount, _, _ = budgetQuote(prompt, output, rates)
		}
		b.reserved[id] = budgetReservation{Tokens: budgetTokenSum(prompt, output), Cost: amount, Currency: currency}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	return id, output, nil
}

func (b *sharedBudget) settle(ctx context.Context, id string, usage *provider.Usage, rates *provider.Pricing) error {
	if id == "" {
		return nil
	}
	if usage == nil || usage.Estimated {
		return nil
	}
	return b.transact(ctx, func() error {
		if _, ok := b.reserved[id]; !ok {
			return nil
		}
		quote := event.EnsureCostQuote(event.Event{Kind: event.Usage, Usage: usage, Pricing: rates}, nil)
		if b.limit.Cost > 0 && (quote == nil || !quote.CostComplete || quote.Original.Currency != b.currency) {
			return nil
		}
		if quote != nil && quote.CostComplete {
			amount, err := pricing.ParseAmount(quote.Original.Amount)
			if err != nil {
				return fmt.Errorf("invalid provider cost: %w", err)
			}
			b.cost[quote.Original.Currency] = b.cost[quote.Original.Currency].Add(amount)
		}
		delete(b.reserved, id)
		b.tokens = budgetTokenSum(b.tokens, budgetTokenSum(max(0, usage.PromptTokens), max(0, usage.CompletionTokens)))
		return nil
	})
}

func budgetTokenSum(a, b int) int {
	if b > math.MaxInt-a {
		return math.MaxInt
	}
	return a + b
}

func budgetedStream(ctx context.Context, p provider.Provider, req *provider.Request, b *sharedBudget, prompt int, rates *provider.Pricing, fixed bool) (<-chan provider.Chunk, error) {
	if !b.limited() {
		return p.Stream(ctx, *req)
	}
	maxOutput := req.MaxTokens
	if maxOutput <= 0 {
		maxOutput = 4096
	}
	id, output, err := b.reserve(ctx, prompt, maxOutput, rates)
	if err != nil {
		return nil, err
	}
	if fixed && output != req.MaxTokens {
		if err := b.transact(ctx, func() error { delete(b.reserved, id); return nil }); err != nil {
			return nil, err
		}
		axis := "token"
		b.mu.Lock()
		if b.limit.Tokens == 0 && b.limit.Cost > 0 {
			axis = "cost"
		}
		b.mu.Unlock()
		return nil, &taskBudgetPause{axis: axis, detail: "remaining allowance cannot replay the unchanged request"}
	}
	req.MaxTokens = output
	stream, err := p.Stream(ctx, *req)
	if err != nil {
		return nil, err
	}
	out := make(chan provider.Chunk)
	go func() {
		defer close(out)
		var usage *provider.Usage
		settled := false
		settle := func() error {
			if settled {
				return nil
			}
			settled = true
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			return b.settle(saveCtx, id, usage, rates)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-stream:
				if !ok {
					if err := settle(); err != nil {
						select {
						case out <- provider.Chunk{Type: provider.ChunkError, Err: err}:
						case <-ctx.Done():
						}
					}
					return
				}
				if chunk.Type == provider.ChunkUsage && chunk.Usage != nil {
					copy := *chunk.Usage
					usage = &copy
				}
				if chunk.Type == provider.ChunkDone {
					if err := settle(); err != nil {
						chunk = provider.Chunk{Type: provider.ChunkError, Err: err}
					}
				}
				if chunk.Type == provider.ChunkError {
					settled = true
				}
				select {
				case out <- chunk:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
