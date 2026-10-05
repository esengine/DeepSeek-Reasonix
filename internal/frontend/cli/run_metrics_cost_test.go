package cli

import (
	"encoding/json"
	"reasonix/internal/contract/pricing"
	"reflect"
	"testing"
)

func money(amount, currency string) pricing.Money {
	return pricing.Money{Amount: amount, Currency: currency}
}

func quote(usd, cny string) pricing.CostQuote {
	selected := money(usd, "USD")
	return pricing.CostQuote{
		Original: money(usd, "USD"),
		Valuations: map[string]pricing.Valuation{
			"USD": {Money: money(usd, "USD"), Basis: "official_table", Source: "https://example.test/pricing", AsOf: "2026-08-28"},
			"CNY": {Money: money(cny, "CNY"), Basis: "official_table", Source: "https://example.test/pricing", AsOf: "2026-08-28"},
		},
		Selected: &selected, BillingMode: "payg", Estimated: true,
		CostComplete: true, DisplayComplete: true, Complete: true,
		DisplayStatus: "matched", AggregateMode: "single_currency",
		ModelRef: "deepseek/deepseek-flash", UsageSource: "executor",
		PricingFingerprint: "5f35aab018a3af3b", CatalogSource: "https://example.test/pricing",
	}
}

// unfold rebuilds the quotes from what the fold wrote. It lives in the test
// because nothing in the product reads the file back — but the fold is only
// worth doing if what it drops is the repetition and not the audit.
func unfold(a *costAudit) []pricing.CostQuote {
	out := make([]pricing.CostQuote, 0, len(a.Calls))
	for _, call := range a.Calls {
		book := a.Books[call.Book]
		q := pricing.CostQuote{
			Original: call.Original, OriginalTotals: call.OriginalTotals, Selected: call.Selected,
			BillingMode: book.BillingMode, Estimated: book.Estimated,
			CostComplete: book.CostComplete, DisplayComplete: book.DisplayComplete,
			Complete: book.Complete, DisplayStatus: book.DisplayStatus,
			AggregateMode: book.AggregateMode, ModelRef: book.ModelRef,
			UsageSource: book.UsageSource, PricingFingerprint: book.PricingFingerprint,
			RateDate: book.RateDate, IncompleteReason: book.IncompleteReason,
			LegacyEstimate: book.LegacyEstimate, CatalogSource: book.CatalogSource,
		}
		for currency, amount := range call.Amounts {
			if q.Valuations == nil {
				q.Valuations = map[string]pricing.Valuation{}
			}
			basis := book.Bases[currency]
			q.Valuations[currency] = pricing.Valuation{
				Money: amount, Basis: basis.Basis, Source: basis.Source,
				AsOf: basis.AsOf, Rate: basis.Rate, Stale: basis.Stale,
			}
		}
		out = append(out, q)
	}
	return out
}

func TestFoldCostQuotesLosesNothing(t *testing.T) {
	quotes := []pricing.CostQuote{quote("0.000395144", "0.0027186"), quote("0.001515952", "0.0103628")}
	folded := foldCostQuotes(quotes)
	if folded == nil {
		t.Fatal("foldCostQuotes = nil")
	}
	if got := unfold(folded); !reflect.DeepEqual(got, quotes) {
		t.Fatalf("round trip changed the quotes:\n got %+v\nwant %+v", got, quotes)
	}
}

// Ten calls against one price book are one book, not ten copies of it.
func TestFoldCostQuotesWritesOneBookPerPriceBook(t *testing.T) {
	quotes := make([]pricing.CostQuote, 10)
	for i := range quotes {
		quotes[i] = quote("0.000395144", "0.0027186")
	}
	folded := foldCostQuotes(quotes)
	if len(folded.Books) != 1 {
		t.Fatalf("books = %d, want 1", len(folded.Books))
	}
	if len(folded.Calls) != 10 {
		t.Fatalf("calls = %d, want 10", len(folded.Calls))
	}
	before, err := json.Marshal(quotes)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(folded)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) >= len(before) {
		t.Fatalf("folded is %d bytes, not smaller than the %d it replaced", len(after), len(before))
	}
	t.Logf("%d bytes -> %d", len(before), len(after))
}

// A second model prices against its own book, and neither one absorbs the other.
func TestFoldCostQuotesSeparatesDifferentPriceBooks(t *testing.T) {
	other := quote("0.002", "0.0138")
	other.ModelRef = "deepseek/deepseek-v4-pro"
	other.PricingFingerprint = "0000aaaa1111bbbb"
	folded := foldCostQuotes([]pricing.CostQuote{quote("0.001", "0.0069"), other})
	if len(folded.Books) != 2 {
		t.Fatalf("books = %d, want 2", len(folded.Books))
	}
	if folded.Calls[0].Book == folded.Calls[1].Book {
		t.Fatal("both calls point at the same book")
	}
}
