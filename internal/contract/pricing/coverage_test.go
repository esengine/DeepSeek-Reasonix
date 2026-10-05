package pricing

import (
	"testing"
	"time"
)

var allCoverages = []string{CoverageNone, CoverageComplete, CoveragePartial, CoverageIncomplete}

// The contract the aggregate has to satisfy, stated as the pairs themselves.
func TestFoldCoverageContract(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{CoverageNone, CoverageNone, CoverageNone},
		{CoverageComplete, CoverageNone, CoverageComplete},
		{CoverageComplete, CoverageComplete, CoverageComplete},
		{CoverageComplete, CoverageIncomplete, CoveragePartial},
		{CoverageIncomplete, CoverageComplete, CoveragePartial},
		{CoverageIncomplete, CoverageIncomplete, CoverageIncomplete},
		{CoveragePartial, CoverageComplete, CoveragePartial},
		{CoveragePartial, CoverageIncomplete, CoveragePartial},
		{CoveragePartial, CoverageNone, CoveragePartial},
	}
	for _, c := range cases {
		if got := FoldCoverage(c.a, c.b); got != c.want {
			t.Errorf("fold(%s, %s) = %s, want %s", c.a, c.b, got, c.want)
		}
	}
}

// Arrival order is not a fact about the session, so it may not reach the
// answer. Exhaustive rather than sampled: there are only sixty-four triples.
func TestFoldCoverageIsOrderIndependent(t *testing.T) {
	for _, a := range allCoverages {
		for _, b := range allCoverages {
			if FoldCoverage(a, b) != FoldCoverage(b, a) {
				t.Errorf("fold(%s, %s) != fold(%s, %s)", a, b, b, a)
			}
			if FoldCoverage(a, a) != a {
				t.Errorf("fold(%s, %s) = %s, want %s", a, a, FoldCoverage(a, a), a)
			}
			for _, c := range allCoverages {
				left := FoldCoverage(FoldCoverage(a, b), c)
				right := FoldCoverage(a, FoldCoverage(b, c))
				if left != right {
					t.Errorf("fold is not associative on (%s, %s, %s): %s vs %s", a, b, c, left, right)
				}
			}
		}
	}
}

// The failure a boolean invites: the last quote deciding for the ones before it.
func TestCompleteNeverSurvivesALaterIncomplete(t *testing.T) {
	for _, a := range allCoverages {
		got := FoldCoverage(FoldCoverage(a, CoverageComplete), CoverageIncomplete)
		if got == CoverageComplete {
			t.Errorf("fold(%s, complete, incomplete) = complete", a)
		}
	}
}

func TestFoldCoverageTreatsAnUnnamedStateAsNothing(t *testing.T) {
	if got := FoldCoverage("", CoverageComplete); got != CoverageComplete {
		t.Fatalf("fold(\"\", complete) = %s", got)
	}
	if got := FoldCoverage("mostly", CoverageIncomplete); got != CoverageIncomplete {
		t.Fatalf("fold(unnamed, incomplete) = %s", got)
	}
}

func pricedQuote(t *testing.T, perMillion float64) CostQuote {
	t.Helper()
	q := BuildQuote(QuoteInput{
		Usage: UsageTokens{PromptTokens: 1000},
		Rates: RateCard{Input: perMillion, Currency: "USD"},
	})
	if q.Coverage != CoverageComplete {
		t.Fatalf("a priced call is not complete: %+v", q)
	}
	return q
}

func unpricedQuote(t *testing.T) CostQuote {
	t.Helper()
	q := NormalizeQuote(CostQuote{
		Estimated: true, DisplayStatus: DisplayStatusUnavailable, IncompleteReason: "no_price",
	})
	if q.Coverage != CoverageIncomplete {
		t.Fatalf("an unpriced call is not incomplete: %+v", q)
	}
	return q
}

func TestAggregateCoverageAcrossCalls(t *testing.T) {
	priced, unpriced := pricedQuote(t, 1), unpricedQuote(t)
	cases := []struct {
		name   string
		quotes []CostQuote
		want   string
	}{
		{"no usage", nil, CoverageNone},
		{"one priced", []CostQuote{priced}, CoverageComplete},
		{"two priced", []CostQuote{priced, priced}, CoverageComplete},
		{"priced then unpriced", []CostQuote{priced, unpriced}, CoveragePartial},
		{"unpriced then priced", []CostQuote{unpriced, priced}, CoveragePartial},
		{"unpriced only", []CostQuote{unpriced}, CoverageIncomplete},
		{"unpriced twice", []CostQuote{unpriced, unpriced}, CoverageIncomplete},
	}
	for _, c := range cases {
		got := AggregateQuotes(c.quotes, "USD")
		if got.Coverage != c.want {
			t.Errorf("%s: coverage = %s, want %s", c.name, got.Coverage, c.want)
		}
		// The boolean is a reading of the coverage, not a second answer.
		if got.CostComplete != (c.want == CoverageComplete) {
			t.Errorf("%s: costComplete = %v against coverage %s", c.name, got.CostComplete, got.Coverage)
		}
	}
}

// A session's completeness is a fact about its calls, so the order they were
// billed in may not change it — at the aggregate, not only in the fold.
func TestAggregateCoverageIgnoresArrivalOrder(t *testing.T) {
	priced, unpriced := pricedQuote(t, 1), unpricedQuote(t)
	forward := AggregateQuotes([]CostQuote{priced, priced, unpriced}, "USD")
	backward := AggregateQuotes([]CostQuote{unpriced, priced, priced}, "USD")
	if forward.Coverage != backward.Coverage || forward.Coverage != CoveragePartial {
		t.Fatalf("order changed coverage: %s vs %s", forward.Coverage, backward.Coverage)
	}
}

// Zero is an amount, not a completeness state. Both sessions total zero; only
// one of them has been billed for anything.
func TestKnownZeroIsCompleteAndNoUsageIsNot(t *testing.T) {
	free := AggregateQuotes([]CostQuote{pricedQuote(t, 0)}, "USD")
	if free.Coverage != CoverageComplete || !free.CostComplete {
		t.Fatalf("a free call priced at zero is not complete: %+v", free)
	}
	if amount := free.Original.AmountValue().Float64(); amount != 0 {
		t.Fatalf("a free call cost %v", amount)
	}
	empty := AggregateQuotes(nil, "USD")
	if empty.Coverage != CoverageNone {
		t.Fatalf("no usage = %s", empty.Coverage)
	}
	if amount := empty.Original.AmountValue().Float64(); amount != 0 {
		t.Fatalf("an empty session cost %v", amount)
	}
	if free.Coverage == empty.Coverage {
		t.Fatal("a billed zero and an unbilled session share one identity")
	}
}

// A quote persisted before coverage existed still has to name one, and the
// name has to be the one its own cost fact supports.
func TestNormalizeFillsCoverageForAQuoteThatPredatesIt(t *testing.T) {
	old := NormalizeQuote(CostQuote{Original: MoneyOf(NewAmountFromFloat(1), "USD"), Complete: true})
	if old.Coverage != CoverageComplete {
		t.Fatalf("an old complete quote = %s", old.Coverage)
	}
	if got := NormalizeQuote(CostQuote{Coverage: "mostly", IncompleteReason: "no_price"}); got.Coverage != CoverageIncomplete {
		t.Fatalf("an unnamed coverage survived normalization: %s", got.Coverage)
	}
	kept := NormalizeQuote(CostQuote{Coverage: CoveragePartial, IncompleteReason: "no_price"})
	if kept.Coverage != CoveragePartial {
		t.Fatalf("normalization overwrote a coverage the quote already had: %s", kept.Coverage)
	}
}

// The ledger merges by pricing key before anything aggregates, so it is a
// second fold. An entry that has seen an unpriced call may not read complete
// again because a later call arrived in another currency.
func TestLedgerEntryKeepsIncompletenessAcrossBucketing(t *testing.T) {
	l := NewLedger()
	unpriced := unpricedQuote(t)
	unpriced.ModelRef, unpriced.UsageSource = "m", "executor"
	cny := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1000}, Rates: RateCard{Input: 1, Currency: "CNY"}, ModelRef: "m", UsageSource: "executor"})
	usd := BuildQuote(QuoteInput{Usage: UsageTokens{PromptTokens: 1000}, Rates: RateCard{Input: 1, Currency: "USD"}, ModelRef: "m", UsageSource: "executor"})
	usd.PricingFingerprint, cny.PricingFingerprint = unpriced.PricingFingerprint, unpriced.PricingFingerprint
	for _, q := range []CostQuote{unpriced, cny, usd} {
		l.Add(q, UsageTokens{PromptTokens: 1000}, time.Time{})
	}
	total := l.Total("USD")
	if total.Coverage != CoveragePartial {
		t.Fatalf("ledger total coverage = %s, want partial", total.Coverage)
	}
	if total.CostComplete {
		t.Fatal("an entry that saw an unpriced call reads as complete")
	}
}
