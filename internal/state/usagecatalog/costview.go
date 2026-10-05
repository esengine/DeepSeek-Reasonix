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

// Priced is the quote a stats line was written with, spelled as the line
// spells it. CNY and USD are the vendor-table valuations, empty when none.
type Priced struct {
	Amount    string
	Currency  string
	CNY       string
	USD       string
	Estimated bool
}

// WithCost attaches a line's quote. An amount that does not parse is dropped
// rather than guessed: a missing total beats a wrong one.
func (e Entry) WithCost(p Priced) Entry {
	e.Cost, e.CostCurrency, e.CostEstimated = 0, "", p.Estimated
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
	return `INSERT INTO usage_cost(day,source,model_ref,view,currency,cost,estimated)
        SELECT day,source,model_ref,'` + view + `',` + currency + `,SUM(COALESCE(` + valuation + `,cost)),MAX(cost_estimated)
        FROM usage_records WHERE day=? AND cost_currency<>'' AND cost<>0 GROUP BY day,source,model_ref,` + currency
}
