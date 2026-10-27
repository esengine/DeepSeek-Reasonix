package event

import (
	"testing"

	"reasonix/internal/contract/pricing"
	"reasonix/internal/contract/provider"
)

func TestEstimatedUsageQuoteSinkCoversTurnAndGuardian(t *testing.T) {
	for _, kind := range []Kind{Usage, GuardianAssessment} {
		t.Run(map[Kind]string{Usage: "turn", GuardianAssessment: "guardian"}[kind], func(t *testing.T) {
			u := &provider.Usage{PromptTokens: 1000, CompletionTokens: 100, Estimated: true}
			rate := &provider.Pricing{Input: 1, Output: 2, Currency: "USD"}
			e := Event{Kind: kind, ModelRef: "custom/model"}
			if kind == Usage {
				e.Usage, e.Pricing = u, rate
			} else {
				e.Guardian.Usage, e.Guardian.Pricing = u, rate
			}
			var got *pricing.CostQuote
			sink := NewCostQuoteSink(FuncSink(func(e Event) {
				got = e.CostQuote
				if kind == GuardianAssessment {
					got = e.Guardian.CostQuote
				}
			}), nil)
			sink.Emit(e)
			if got == nil || got.CostComplete || got.Coverage != pricing.CoverageIncomplete || got.IncompleteReason != "missing_price_or_usage" || got.Original.IsZero() {
				t.Fatalf("sink quote = %+v", got)
			}
			if !u.Estimated || u.PromptTokens != 1000 || u.CompletionTokens != 100 {
				t.Fatalf("input usage mutated: %+v", u)
			}
		})
	}
}
