package config

import (
	"testing"

	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
)

func TestEstimatedUsageConfiguredQuote(t *testing.T) {
	price := &provider.Pricing{Input: 1, Output: 2, Currency: "USD"}
	u := &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, Estimated: true}
	q := QuoteForUsage(price, u, "USD", "custom/model", "compaction", pricing.BillingModePAYG, "")
	if q.CostComplete || q.Coverage != pricing.CoverageIncomplete || q.Original.Amount != "0.0012" || q.Selected != nil {
		t.Fatalf("estimated configured quote = %+v", q)
	}
	u.Estimated = false
	control := QuoteForUsage(price, u, "USD", "custom/model", "compaction", pricing.BillingModePAYG, "")
	if !control.CostComplete || control.Original != q.Original || control.Coverage != pricing.CoverageComplete {
		t.Fatalf("reported configured quote = %+v, estimated %+v", control, q)
	}
}
