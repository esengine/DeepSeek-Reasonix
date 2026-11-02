package agent

import (
	"testing"

	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
)

func TestLocalCostKeepsOriginalCurrencyBuckets(t *testing.T) {
	var b runBudget
	u := &provider.Usage{PromptTokens: 1, RequestCount: 1}
	observe := func(amount, currency string) {
		q := &pricing.CostQuote{Original: pricing.Money{Amount: amount, Currency: currency}, CostComplete: true, Coverage: pricing.CoverageComplete, Selected: &pricing.Money{Amount: "999", Currency: "EUR"}}
		b.observe(u, q)
		q.Original.Amount = "999"
	}
	observe("0.4", "CNY")
	observe("0.3", "USD")
	if b.cost.amount() != pricing.NewAmountFromFloat(.3) || b.totals().Priced {
		t.Fatalf("mixed totals=%+v", b.totals())
	}
	if axis, _ := b.exceeded(TaskBudget{Cost: .5}); axis != "" {
		t.Fatal("mixed currencies or selected display triggered gate")
	}
	observe("0.2", "CNY")
	if axis, _ := b.exceeded(TaskBudget{Cost: .5}); axis != "cost" {
		t.Fatal("known CNY subtotal did not cross")
	}
	if b.cost.amounts["USD"] != pricing.NewAmountFromFloat(.3) {
		t.Fatal("currency switch lost prior bucket")
	}
}

func TestLocalCostFixedPointThresholdAndUnknown(t *testing.T) {
	var b runBudget
	u := &provider.Usage{PromptTokens: 1, RequestCount: 1}
	for _, amount := range []string{"0.1", "0.2"} {
		b.observe(u, &pricing.CostQuote{Original: pricing.Money{Amount: amount, Currency: "USD"}, CostComplete: true, Coverage: pricing.CoverageComplete})
	}
	if axis, _ := b.exceeded(TaskBudget{Cost: .3}); axis != "cost" {
		t.Fatal("decimal threshold missed")
	}
	b.observe(u, nil)
	if axis, _ := b.exceeded(TaskBudget{Cost: .3}); axis != "cost" || b.totals().Priced {
		t.Fatal("unknown usage erased known subtotal or claimed completeness")
	}
	var zero runBudget
	z := &provider.Usage{}
	zero.observe(z, budgetTestQuote(z, &provider.Pricing{Currency: "USD", Input: 1}))
	if axis, _ := zero.exceeded(TaskBudget{Cost: 1e-12}); axis != "" {
		t.Fatal("rounded threshold charged zero usage")
	}
}
