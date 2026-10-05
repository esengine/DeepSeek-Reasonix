// Folding the run's cost quotes for the metrics file: the price book each call
// was quoted against, written once, and the amounts written per call.
package cli

import (
	"encoding/json"
	"reasonix/internal/contract/pricing"
)

// costAudit is the run's quotes with the price book lifted out of them. Every
// quote repeats the catalogue, the fingerprint, the model and how each currency
// was reached; only the amounts differ, and for a single-model run that is one
// book and n numbers instead of n copies of the same paragraph.
type costAudit struct {
	Books []costBook `json:"books"`
	Calls []costCall `json:"calls"`
}

// costBook is a quote with its amounts removed. Two calls share one when every
// fact except the numbers matched, so nothing is folded away that could differ.
type costBook struct {
	ModelRef           string `json:"modelRef,omitempty"`
	UsageSource        string `json:"usageSource,omitempty"`
	PricingFingerprint string `json:"pricingFingerprint,omitempty"`
	CatalogSource      string `json:"catalogSource,omitempty"`
	BillingMode        string `json:"billingMode,omitempty"`
	DisplayStatus      string `json:"displayStatus,omitempty"`
	AggregateMode      string `json:"aggregateMode,omitempty"`
	RateDate           string `json:"rateDate,omitempty"`
	IncompleteReason   string `json:"incompleteReason,omitempty"`
	Estimated          bool   `json:"estimated,omitempty"`
	CostComplete       bool   `json:"costComplete,omitempty"`
	DisplayComplete    bool   `json:"displayComplete,omitempty"`
	Complete           bool   `json:"complete,omitempty"`
	LegacyEstimate     bool   `json:"legacyEstimate,omitempty"`
	// Bases is how each currency was reached, keyed by ISO code.
	Bases map[string]costBasis `json:"bases,omitempty"`
}

// costBasis is one currency's derivation, which belongs to the book rather than
// to any one call that read it.
type costBasis struct {
	Basis  string                `json:"basis,omitempty"`
	Source string                `json:"source,omitempty"`
	AsOf   string                `json:"asOf,omitempty"`
	Rate   *pricing.RateSnapshot `json:"rateSnapshot,omitempty"`
	Stale  bool                  `json:"stale,omitempty"`
}

// costCall is one model call: which book priced it, and what it came to.
type costCall struct {
	Book           int             `json:"book"`
	Original       pricing.Money   `json:"original"`
	OriginalTotals []pricing.Money `json:"originalTotals,omitempty"`
	Selected       *pricing.Money  `json:"selected,omitempty"`
	// Amounts is the per-currency valuation, keyed the way Bases is.
	Amounts map[string]pricing.Money `json:"amounts,omitempty"`
}

// foldCostQuotes splits quotes into the books they were priced against and the
// per-call amounts. It is lossless: every field of a quote lands in exactly one
// of the two, which is what lets the metrics file drop the repetition without
// dropping the audit.
func foldCostQuotes(quotes []pricing.CostQuote) *costAudit {
	if len(quotes) == 0 {
		return nil
	}
	out := &costAudit{Calls: make([]costCall, 0, len(quotes))}
	index := map[string]int{}
	for _, q := range quotes {
		book := bookOf(q)
		key, err := json.Marshal(book)
		if err != nil {
			continue
		}
		at, ok := index[string(key)]
		if !ok {
			at = len(out.Books)
			index[string(key)] = at
			out.Books = append(out.Books, book)
		}
		out.Calls = append(out.Calls, callOf(q, at))
	}
	if len(out.Calls) == 0 {
		return nil
	}
	return out
}

func bookOf(q pricing.CostQuote) costBook {
	book := costBook{
		ModelRef: q.ModelRef, UsageSource: q.UsageSource,
		PricingFingerprint: q.PricingFingerprint, CatalogSource: q.CatalogSource,
		BillingMode: q.BillingMode, DisplayStatus: q.DisplayStatus,
		AggregateMode: q.AggregateMode, RateDate: q.RateDate,
		IncompleteReason: q.IncompleteReason, Estimated: q.Estimated,
		CostComplete: q.CostComplete, DisplayComplete: q.DisplayComplete,
		Complete: q.Complete, LegacyEstimate: q.LegacyEstimate,
	}
	for currency, valuation := range q.Valuations {
		if book.Bases == nil {
			book.Bases = map[string]costBasis{}
		}
		basis := costBasis{
			Basis: valuation.Basis, Source: valuation.Source,
			AsOf: valuation.AsOf, Stale: valuation.Stale,
		}
		// The only pointer that would otherwise outlive the snapshot's lock.
		if valuation.Rate != nil {
			rate := *valuation.Rate
			basis.Rate = &rate
		}
		book.Bases[currency] = basis
	}
	return book
}

func callOf(q pricing.CostQuote, book int) costCall {
	call := costCall{Book: book, Original: q.Original}
	if len(q.OriginalTotals) > 0 {
		call.OriginalTotals = append([]pricing.Money(nil), q.OriginalTotals...)
	}
	if q.Selected != nil {
		selected := *q.Selected
		call.Selected = &selected
	}
	for currency, valuation := range q.Valuations {
		if call.Amounts == nil {
			call.Amounts = map[string]pricing.Money{}
		}
		call.Amounts[currency] = valuation.Money
	}
	return call
}
