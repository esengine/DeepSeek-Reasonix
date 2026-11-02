package usagecatalog

import (
	"strings"

	"reasonix/internal/contract/pricing"
)

// costViews are the currencies a cost is tallied in: "" is each row's billed
// currency, the others are the currencies vendors publish list prices in. Every
// view partitions the same rows, so no row is counted twice within one.
var costViews = []string{"", "CNY", "USD"}

// CostView names the view a display currency reads. A currency no view is kept
// for reads the billed amounts, as does an empty preference.
func CostView(currency string) string {
	currency = pricing.NormalizeCurrency(currency)
	for _, view := range costViews {
		if view == currency {
			return view
		}
	}
	return ""
}

// ModeEstimate is the cost mode of a subscription-equivalent quote: an
// API-priced estimate of what a plan covered, not money billed.
const ModeEstimate = "estimate"

// CostMode is the cost mode a billing mode files its quotes under: "" is billed.
func CostMode(billingMode string) string {
	if billingMode == pricing.BillingModeSubscriptionEquivalent {
		return ModeEstimate
	}
	return ""
}

// Priced is the quote a stats line was written with, spelled as the line
// spells it. CNY and USD are the vendor-table valuations, empty when none.
type Priced struct {
	Amount    string
	Currency  string
	CNY       string
	USD       string
	Estimated bool
	// Incomplete says the quote could not price the row's usage: its money is
	// unknown, not zero.
	Incomplete bool
	// Reason is the line's incomplete_reason, already normalized by the caller
	// through UnpricedReason; empty when the row recorded none.
	Reason string
	// BillingMode is the mode the quote was made in; empty reads as billed.
	BillingMode string
}

// CostIncomplete reports whether a line that counted usage has no cost fact:
// a quote that said it could not price the call, or no amount at all — a row
// written before the cost fields existed, or one that simply lacks them. Such
// a row's zero money is unknown, not free, so a total folding it in is a floor.
// A turn marker counts no usage, so it is never an unpriced call.
func CostIncomplete(costComplete *bool, total int, amount string) bool {
	if costComplete != nil && !*costComplete {
		return true
	}
	return total > 0 && strings.TrimSpace(amount) == ""
}

// ReasonNoPrice is the reason the quote path already records for a call with
// no price fact (event.EnsureCostQuote). A row written before the cost fields
// existed carries no reason at all; this names the case it is.
const ReasonNoPrice = "no_price"

// UnpricedReason is CostIncomplete plus the reason behind it, in the
// vocabulary the quote already writes. An unpriced row that recorded no reason
// is still unpriced; only a row with no cost fact takes the existing
// ReasonNoPrice, and no reason is invented for the others.
func UnpricedReason(costComplete *bool, total int, amount, reason string) (string, bool) {
	if !CostIncomplete(costComplete, total, amount) {
		return "", false
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		return reason, true
	}
	if strings.TrimSpace(amount) == "" {
		return ReasonNoPrice, true
	}
	return "", true
}

// WithCost attaches a line's quote. An amount that does not parse is dropped
// rather than guessed: a missing total beats a wrong one.
func (e Entry) WithCost(p Priced) Entry {
	e.Cost, e.CostCurrency, e.CostEstimated, e.CostIncomplete, e.BillingMode = 0, "", p.Estimated, p.Incomplete, p.BillingMode
	e.IncompleteReason = ""
	if p.Incomplete {
		e.IncompleteReason = p.Reason
	}
	if amount, ok := parseAmount(p.Amount); ok {
		e.Cost, e.CostCurrency = amount, pricing.NormalizeCurrency(p.Currency)
	}
	e.Valuations = nil
	for currency, raw := range map[string]string{"CNY": p.CNY, "USD": p.USD} {
		if amount, ok := parseAmount(raw); ok {
			if e.Valuations == nil {
				e.Valuations = map[string]int64{}
			}
			e.Valuations[currency] = amount
		}
	}
	return e
}

// CostIn is the row's cost as a view reads it: the vendor table's amount in
// the view's currency when one was quoted, otherwise what was billed.
func (e Entry) CostIn(view string) (string, int64) {
	if amount, ok := e.Valuations[view]; ok && view != "" {
		return view, amount
	}
	return e.CostCurrency, e.Cost
}

func parseAmount(raw string) (int64, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, false
	}
	amount, err := pricing.ParseAmount(raw)
	if err != nil {
		return 0, false
	}
	return int64(amount), true
}

func valuationArg(e Entry, currency string) any {
	if amount, ok := e.Valuations[currency]; ok {
		return amount
	}
	return nil
}

// viewCostSelect derives one view's tallies for a day from its records.
func viewCostSelect(view string) string {
	valuation := "NULL"
	if view != "" {
		valuation = "valuation_" + strings.ToLower(view)
	}
	currency := "CASE WHEN " + valuation + " IS NULL THEN cost_currency ELSE '" + view + "' END"
	mode := "CASE WHEN billing_mode='" + pricing.BillingModeSubscriptionEquivalent + "' THEN '" + ModeEstimate + "' ELSE '' END"
	// cost_currency<>'' is the whole test for "priced": a priced line whose
	// amount is 0 is free, not unknown, and keeps its currency, its zero and
	// its estimate flag in the money the JSONL read reports.
	return `INSERT INTO usage_cost(day,source,model_ref,view,currency,mode,cost,estimated)
        SELECT day,source,model_ref,'` + view + `',` + currency + `,` + mode + `,SUM(COALESCE(` + valuation + `,cost)),MAX(cost_estimated)
        FROM usage_records WHERE day=? AND cost_currency<>'' GROUP BY day,source,model_ref,` + currency + `,` + mode
}
