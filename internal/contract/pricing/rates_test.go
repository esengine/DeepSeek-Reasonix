package pricing

import "testing"

// The history answers one question: is a stored price the current default, or a
// superseded one this project may replace? A generation repeating another in the
// same row makes that unanswerable — the current rate would match as superseded,
// and every refresh would rewrite it while reporting that something changed.
func TestRateHistoryHasNoRepeatedGenerations(t *testing.T) {
	for _, row := range officialRates {
		if len(row.Rates) == 0 {
			t.Errorf("%s/%s/%s has no rate at all", row.Provider, row.Model, row.Currency)
			continue
		}
		seen := map[RateCard]bool{}
		for _, generation := range row.Rates {
			if seen[generation.Base] {
				t.Errorf("%s/%s/%s repeats the rate %+v", row.Provider, row.Model, row.Currency, generation.Base)
			}
			seen[generation.Base] = true
			if generation.Base.Currency != row.Currency {
				t.Errorf("%s/%s/%s holds a rate filed as %q", row.Provider, row.Model, row.Currency, generation.Base.Currency)
			}
		}
	}
}

// CurrentRate returns the first row it matches, so two rows sharing all four
// identifying fields make which rate a caller gets depend on table order.
func TestOfficialRateRowsAreUniquelyIdentified(t *testing.T) {
	type key struct{ provider, model, currency, mode string }
	seen := map[key]bool{}
	for _, row := range officialRates {
		k := key{row.Provider, row.Model, row.Currency, row.BillingMode}
		if seen[k] {
			t.Errorf("%+v appears twice; which rate CurrentRate returns is then a matter of order", k)
		}
		seen[k] = true
	}
}

// Superseded means "no longer charged". Returning the current rate among them
// would make every refresh a no-op that still reports having changed something.
func TestSupersededRatesExcludeTheCurrentOne(t *testing.T) {
	for _, row := range officialRates {
		current := CurrentRate(row.Provider, row.Model, row.Currency)
		if current == nil {
			if row.BillingMode != BillingModeSubscriptionEquivalent {
				t.Errorf("%s/%s/%s has no current rate", row.Provider, row.Model, row.Currency)
			}
			continue
		}
		for _, superseded := range SupersededRates(row.Provider, row.Model, row.Currency) {
			if superseded == *current {
				t.Errorf("%s/%s/%s lists its current rate as superseded", row.Provider, row.Model, row.Currency)
			}
		}
	}
}

// A retired name bills at the flash rate, which is the whole reason the catalog
// publishes a row for it; resolving it anywhere else would price it as nothing.
func TestRetiredDeepSeekNamesBillAtTheFlashRate(t *testing.T) {
	for _, retired := range RetiredDeepSeekFlashModels() {
		for _, currency := range []string{"CNY", "USD"} {
			got := CurrentRate("deepseek", retired, currency)
			want := CurrentRate("deepseek", "deepseek-flash", currency)
			if got == nil || want == nil || *got != *want {
				t.Errorf("%s/%s = %+v, want the flash rate %+v", retired, currency, got, want)
			}
		}
	}
}

// Every row the table declares has to reach the catalog, which is what a quote
// actually reads; a rate declared but never published quotes as nothing.
func TestEveryRateRowReachesTheCatalog(t *testing.T) {
	published := map[string]bool{}
	for _, entry := range OfficialCatalog() {
		published[entry.Provider+"/"+entry.Model+"/"+entry.Currency+"/"+entry.BillingMode] = true
	}
	for _, row := range officialRates {
		if !published[row.Provider+"/"+row.Model+"/"+row.Currency+"/"+row.BillingMode] {
			t.Errorf("%s/%s/%s/%s is declared but never published", row.Provider, row.Model, row.Currency, row.BillingMode)
		}
	}
}
