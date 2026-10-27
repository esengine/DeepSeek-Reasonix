package pricing

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEstimatedUsageQuotePreservesAmountsWithoutCompleteFacts(t *testing.T) {
	for _, currency := range []string{"CNY", "USD"} {
		t.Run(currency, func(t *testing.T) {
			in := testInput(currency, "deepseek", "deepseek-v4-flash")
			in.OccurredAt = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			exact := BuildQuote(in)
			if !exact.Estimated || !exact.CostComplete || exact.Coverage != CoverageComplete {
				t.Fatalf("reported usage at estimated rates = %+v", exact)
			}
			in.Usage.Estimated = true
			q := BuildQuote(in)
			if q.CostComplete || q.DisplayComplete || q.Complete || q.Selected != nil || q.Coverage != CoverageIncomplete || q.IncompleteReason != "missing_price_or_usage" {
				t.Fatalf("reconstructed usage = %+v", q)
			}
			if q.Original != exact.Original || q.Original.IsZero() || len(q.Valuations) != len(exact.Valuations) {
				t.Fatalf("estimated amounts lost: got %+v, reported control %+v", q, exact)
			}
			for code, v := range exact.Valuations {
				if q.Valuations[code] != v {
					t.Fatalf("valuation %s changed: %+v / %+v", code, q.Valuations[code], v)
				}
			}
			q.Coverage = ""
			if got := NormalizeQuote(q); got.Coverage != CoverageIncomplete {
				t.Fatalf("normalization certified estimated usage: %+v", got)
			}
			if got := q.WithSelected(currency); got.Complete || got.IncompleteReason != "missing_price_or_usage" {
				t.Fatalf("display selection certified estimated usage: %+v", got)
			}
		})
	}
}

func TestEstimatedUsageLedgerRetainsPartialCoverage(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate_keys", true: "same_key"}[sameKey], func(t *testing.T) {
			in := testInput("USD", "", "custom")
			in.OccurredAt = time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
			exact := BuildQuote(in)
			in.Usage.Estimated = true
			if !sameKey {
				in.UsageSource = "guardian"
			}
			uncertain := BuildQuote(in)
			ledger := NewLedger()
			ledger.Add(exact, UsageTokens{PromptTokens: 1}, in.OccurredAt)
			ledger.Add(uncertain, UsageTokens{PromptTokens: 1, Estimated: true}, in.OccurredAt)
			raw, err := json.Marshal(ledger)
			if err != nil {
				t.Fatal(err)
			}
			var reopened Ledger
			if err := json.Unmarshal(raw, &reopened); err != nil {
				t.Fatal(err)
			}
			reopened.Add(exact, UsageTokens{PromptTokens: 1}, in.OccurredAt)
			for _, display := range []string{"", "USD", "CNY"} {
				q := reopened.SelectDisplay(display)
				if q.CostComplete || q.Complete || q.DisplayComplete || q.Selected != nil || q.Coverage != CoveragePartial {
					t.Fatalf("display %q upgraded partial coverage: %+v", display, q)
				}
				want := exact.Original.AmountValue().Add(uncertain.Original.AmountValue()).Add(exact.Original.AmountValue())
				if q.Original.AmountValue() != want {
					t.Fatalf("original subtotal = %s, want %s", q.Original.Amount, want)
				}
			}
		})
	}
}

func TestEstimatedUsageSavedQuoteReader(t *testing.T) {
	raw := `{"original":{"amount":"0.0012","currency":"USD"},"valuations":{"USD":{"money":{"amount":"0.0012","currency":"USD"},"basis":"identity"}},"estimated":true,"costComplete":false,"coverage":"incomplete","displayComplete":false,"complete":false,"displayStatus":"unavailable","incompleteReason":"missing_price_or_usage"}`
	var q CostQuote
	if err := json.Unmarshal([]byte(raw), &q); err != nil {
		t.Fatal(err)
	}
	q = NormalizeQuote(q)
	if q.CostComplete || q.Complete || q.Coverage != CoverageIncomplete || q.Original.Amount != "0.0012" {
		t.Fatalf("saved quote = %+v", q)
	}
	if got := q.WithSelected("USD"); got.Complete || got.IncompleteReason != "missing_price_or_usage" {
		t.Fatalf("selected saved quote = %+v", got)
	}
	control := NormalizeQuote(CostQuote{Original: q.Original, Complete: true})
	if !control.CostComplete || control.Coverage != CoverageComplete {
		t.Fatalf("legacy complete quote = %+v", control)
	}
}
