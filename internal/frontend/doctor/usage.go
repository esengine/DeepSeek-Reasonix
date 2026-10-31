package doctor

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/pricing"
	"reasonix/internal/state/stats"
)

// UsageProvenance is what `reasonix doctor usage` answers about one usage
// range: what the money total is over, how many of its calls have no price and
// why, how each price was reached, and whether the figure is a bill or only a
// floor. It is a read of the same aggregate the usage page renders.
type UsageProvenance struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Source   string `json:"source"`
	Currency string `json:"currency"`
	// CountedCalls is the denominator the other counts are read against.
	CountedCalls int `json:"countedCalls"`
	// UnpricedCalls is how many of those calls have no price, and
	// IncompleteReasons is the distinct vocabulary their rows recorded.
	UnpricedCalls     int      `json:"unpricedCalls"`
	IncompleteReasons []string `json:"incompleteReasons"`
	// EstimatedCalls is how many priced calls were quoted from a fallback
	// rate card rather than a provider-reported amount.
	EstimatedCalls int `json:"estimatedCalls"`
	// UnnamedCardCalls is how many counted calls recorded a price without
	// naming its card. It is null when the range cannot classify every call.
	UnnamedCardCalls *int `json:"unnamedCardCalls"`
	// UnclassifiedCardCalls is how many counted calls recorded no price fact
	// at all: empty is unknown here, never zero.
	UnclassifiedCardCalls int `json:"unclassifiedCardCalls"`
	// Cost is billed money, one entry per currency; it is a floor when
	// UnpricedCalls or UnclassifiedCardCalls is non-zero. EstimateCost is a
	// subscription-equivalent estimate and is never added to Cost.
	Cost         []pricing.Money `json:"cost"`
	EstimateCost []pricing.Money `json:"estimateCost"`
	// Money is the one-line answer to what the figure is: a bill or a floor.
	Money string `json:"money"`
}

// CollectUsageProvenance reads the range's aggregate and turns it into the
// audit's four answers. The totals come from stats.Writer.Query, the one
// reader the usage page uses; nothing here scans the files again.
func CollectUsageProvenance(f stats.SourceFilter) (UsageProvenance, error) {
	report, err := stats.NewWriter(config.StatsDir()).Query(f)
	if err != nil {
		return UsageProvenance{}, err
	}
	source := strings.TrimSpace(f.Source)
	if source == "" {
		source = "all"
	}
	rep := UsageProvenance{
		From: report.From, To: report.To, Source: source,
		Currency:              usageCurrencyLabel(f.Currency),
		CountedCalls:          report.CountedCalls,
		UnpricedCalls:         report.UnpricedCalls,
		IncompleteReasons:     append([]string{}, report.IncompleteReasons...),
		EstimatedCalls:        report.EstimatedCalls,
		UnclassifiedCardCalls: report.UnclassifiedCardCalls,
		Cost:                  append([]pricing.Money{}, report.Cost...),
		EstimateCost:          append([]pricing.Money{}, report.EstimateCost...),
	}
	// A range that could not classify every call cannot say how many remain
	// unnamed. Publishing the classified subset as the total would turn an
	// unknown into a measured zero.
	if report.UnclassifiedCardCalls == 0 {
		unnamed := report.UnnamedCardCalls
		rep.UnnamedCardCalls = &unnamed
	}
	rep.Money = usageMoneyStatement(report)
	return rep, nil
}

func usageCurrencyLabel(currency string) string {
	if currency = pricing.NormalizeCurrency(currency); currency == "" {
		return "auto"
	}
	return currency
}

// usageMoneyStatement is the one-line answer to whether the unknown could
// change the conclusion: a floor leaves room above it, a bill does not.
func usageMoneyStatement(r stats.RangeStats) string {
	switch {
	case r.CountedCalls == 0:
		return "none — the range counted no calls (a measured zero)"
	case r.UnpricedCalls > 0:
		return fmt.Sprintf("a floor, not a bill: %d of %d counted calls have no price, so the true cost is at least this",
			r.UnpricedCalls, r.CountedCalls)
	case r.UnclassifiedCardCalls > 0:
		return fmt.Sprintf("a floor, not a bill: %d of %d counted calls record no usable price fact, so the true cost is at least this",
			r.UnclassifiedCardCalls, r.CountedCalls)
	case len(r.Cost) == 0:
		return fmt.Sprintf("no bill: all %d counted calls are subscription-equivalent estimates, which are not money paid", r.CountedCalls)
	case r.EstimatedCalls > 0:
		return fmt.Sprintf("a bill, partly estimated: %d of %d counted calls were priced from a fallback rate card",
			r.EstimatedCalls, r.CountedCalls)
	case r.UnnamedCardCalls > 0:
		return fmt.Sprintf("a bill: every counted call was priced, though %d of them name no rate card", r.UnnamedCardCalls)
	default:
		return fmt.Sprintf("a bill: all %d counted calls were priced", r.CountedCalls)
	}
}

// RenderUsageProvenanceText prints the audit. Every count is labelled with
// what it is measured against, and a count the range cannot classify says
// unknown with the rows it could not read rather than a zero.
func RenderUsageProvenanceText(r UsageProvenance) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage provenance — %s..%s · source: %s · currency: %s\n", r.From, r.To, r.Source, r.Currency)
	fmt.Fprintf(&b, "calls counted: %d — the denominator: usage rows that carried tokens or a price fact\n", r.CountedCalls)
	if r.UnpricedCalls == 0 {
		fmt.Fprintf(&b, "calls unpriced: 0 of %d (measured)\n", r.CountedCalls)
	} else {
		fmt.Fprintf(&b, "calls unpriced: %d of %d — their money is unknown\n", r.UnpricedCalls, r.CountedCalls)
		if len(r.IncompleteReasons) == 0 {
			b.WriteString("  reasons: none recorded\n")
		} else {
			fmt.Fprintf(&b, "  reasons: %s\n", strings.Join(r.IncompleteReasons, ", "))
		}
	}
	if r.EstimatedCalls == 0 {
		fmt.Fprintf(&b, "calls priced from an estimated/fallback card: 0 of %d (measured)\n", r.CountedCalls)
	} else {
		fmt.Fprintf(&b, "calls priced from an estimated/fallback card: %d of %d\n", r.EstimatedCalls, r.CountedCalls)
	}
	switch {
	case r.UnnamedCardCalls == nil:
		fmt.Fprintf(&b, "calls whose rate card is not named: unknown — %d of %d counted calls record no price fact, so whether their card was named was never recorded (not 0)\n",
			r.UnclassifiedCardCalls, r.CountedCalls)
	case *r.UnnamedCardCalls == 0:
		fmt.Fprintf(&b, "calls whose rate card is not named: 0 of %d (measured)\n", r.CountedCalls)
	default:
		fmt.Fprintf(&b, "calls whose rate card is not named: %d of %d\n", *r.UnnamedCardCalls, r.CountedCalls)
	}
	fmt.Fprintf(&b, "money: %s — %s\n", usageMoneyAmounts(r.Cost), r.Money)
	if len(r.EstimateCost) > 0 {
		fmt.Fprintf(&b, "estimate, not a bill: %s — subscription-equivalent use, never added to the bill\n", usageMoneyAmounts(r.EstimateCost))
	}
	return b.String()
}

func usageMoneyAmounts(money []pricing.Money) string {
	if len(money) == 0 {
		return "none recorded"
	}
	parts := make([]string, 0, len(money))
	for _, m := range money {
		parts = append(parts, m.Amount+" "+m.Currency)
	}
	if len(parts) > 1 {
		return strings.Join(parts, ", ") + " (per currency, never summed)"
	}
	return parts[0]
}
