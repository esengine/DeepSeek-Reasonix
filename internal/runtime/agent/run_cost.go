package agent

import (
	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
)

type runCost struct {
	amounts      map[string]pricing.Amount
	currency     string
	pricedRounds int
	incomplete   bool
}

func (c *runCost) observe(usage *provider.Usage, quote *pricing.CostQuote) {
	if quote != nil && quote.Original.Currency != "" {
		c.currency = pricing.NormalizeCurrency(quote.Original.Currency)
	}
	if usage == nil || usage.Estimated || quote == nil || pricing.NormalizeCurrency(quote.Original.Currency) == "" || len(quote.OriginalTotals) > 0 {
		c.incomplete = true
		return
	}
	amount, err := pricing.NewAmountFromString(quote.Original.Amount)
	if err != nil || amount < 0 {
		c.incomplete = true
		return
	}
	q := pricing.NormalizeQuote(*quote)
	// An explicitly reported zero retains its known zero subtotal even though
	// the quote layer classifies a no-token record as incomplete usage.
	zero := usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 &&
		usage.CacheHitTokens == 0 && usage.CacheMissTokens == 0 && usage.CacheWriteTokens == 0 && usage.CacheWriteBilledTokens == 0
	if !q.CostComplete && !(zero && q.IncompleteReason == "missing_price_or_usage" && amount == 0) {
		c.incomplete = true
		return
	}
	if c.amounts == nil {
		c.amounts = map[string]pricing.Amount{}
	}
	c.amounts[c.currency] = c.amounts[c.currency].Add(amount)
	c.pricedRounds++
}

func (c *runCost) amount() pricing.Amount { return c.amounts[c.currency] }

func (c *runCost) complete() bool {
	return !c.incomplete && c.pricedRounds > 0 && len(c.amounts) == 1
}
