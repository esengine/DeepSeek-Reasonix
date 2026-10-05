package pricing

import (
	"net/url"
	"strings"
	"time"
)

// Official catalog metadata: currency, model, effective dates, documentation
// sources, and price fingerprints. User-custom prices always win over catalog.

// CatalogEntry is one official list price for a model in a billing currency.
type CatalogEntry struct {
	Provider      string // deepseek | longcat | mimo
	Model         string
	Currency      string // ISO billing currency for this row
	CacheHit      float64
	Input         float64
	Output        float64
	EffectiveFrom string // YYYY-MM-DD inclusive, empty = unbounded
	EffectiveTo   string // YYYY-MM-DD exclusive, empty = unbounded
	DocURL        string
	BillingMode   string // payg | subscription_equivalent
	Notes         string
	Fingerprint   string // filled by Register
	// Peak is what the same tokens cost inside Window. Nil when the vendor
	// bills one rate around the clock, and then the fields above are the rate.
	Peak   *RateCard
	Window *PeakWindow
	// CheckedOn is when a person last read DocURL and confirmed these numbers.
	// No vendor here serves prices over an API, so this date is all that stands
	// between a price change and a readout that is quietly wrong.
	CheckedOn string
}

// deepseekPeak is the schedule DeepSeek publishes: peak on weekday mornings and
// afternoons, and — since 2026-08-23 — never at a weekend. Chinese public
// holidays bill off-peak in full, per the same page.
var deepseekPeak = &PeakWindow{
	OffsetSeconds:  beijing,
	Hours:          [][2]int{{9, 12}, {14, 18}},
	WeekendOffPeak: "2026-08-23",
	HolidayOffPeak: chineseStatutoryHolidays,
}

// chineseStatutoryHolidays are the State Council's 2026 public holidays, which
// DeepSeek bills off-peak in full; extend it when the next notice is out. Make-up
// workdays are absent: they fall on weekends, which are off-peak anyway.
var chineseStatutoryHolidays = []string{
	// New Year's Day: Jan 1-3.
	"2026-01-01", "2026-01-02", "2026-01-03",
	// Spring Festival: Feb 15-23.
	"2026-02-15", "2026-02-16", "2026-02-17", "2026-02-18", "2026-02-19",
	"2026-02-20", "2026-02-21", "2026-02-22", "2026-02-23",
	// Qingming Festival: Apr 4-6.
	"2026-04-04", "2026-04-05", "2026-04-06",
	// Labor Day: May 1-5.
	"2026-05-01", "2026-05-02", "2026-05-03", "2026-05-04", "2026-05-05",
	// Dragon Boat Festival: Jun 19-21.
	"2026-06-19", "2026-06-20", "2026-06-21",
	// Mid-Autumn Festival: Sep 25-27.
	"2026-09-25", "2026-09-26", "2026-09-27",
	// National Day: Oct 1-7.
	"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04",
	"2026-10-05", "2026-10-06", "2026-10-07",
}

// checkedOn is when the tables below were last read off the vendors' pages.
const checkedOn = "2026-08-22"

// deepseekCheckedOn is when the DeepSeek rows were last read off that vendor's
// page. Separate from checkedOn, because confirming one vendor's prices says
// nothing about another's and a shared date would claim that it did.
const deepseekCheckedOn = "2026-09-13"

// CatalogSourceURLs document public pricing pages.
const (
	DocDeepSeekPricing   = "https://api-docs.deepseek.com/quick_start/pricing"
	DocLongCatPricingUSD = "https://longcat.chat/platform/docs/Pricing/LongCat-2.0.html"
	DocLongCatPricingCNY = "https://longcat.chat/platform/docs/zh/pricing/long-cat-2.0"
	DocMiMoPAYG          = "https://mimo.mi.com/docs/price/pay-as-you-go"
	DocMiMoTokenPlan     = "https://platform.xiaomimimo.com/token-plan"
)

// officialEndpointHosts maps a vendor's own API hostnames to the catalog
// provider whose list prices apply there. Host equality is the test: a reseller
// or proxy bills on its own terms even when it serves the same model, and an
// entry merely named after a vendor is not evidence of the vendor's endpoint.
var officialEndpointHosts = map[string]string{
	"api.deepseek.com":              "deepseek",
	"api.longcat.chat":              "longcat",
	"api.xiaomimimo.com":            "mimo",
	"token-plan-cn.xiaomimimo.com":  "mimo",
	"token-plan-sgp.xiaomimimo.com": "mimo",
	"token-plan-ams.xiaomimimo.com": "mimo",
}

// OfficialProviderForEndpoint returns the catalog provider whose official
// prices apply to baseURL, or "" when the address is not a vendor's own API.
func OfficialProviderForEndpoint(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return officialEndpointHosts[strings.ToLower(u.Hostname())]
}

// OfficialCatalog is the built-in price book: the current generation of every
// rate in officialRates, fingerprinted so a stored price can be recognised as
// one of ours. User-custom prices always win over it.
func OfficialCatalog() []CatalogEntry {
	entries := officialCatalogRows()
	for i := range entries {
		entries[i].Fingerprint = PricingFingerprint(RateCard{
			CacheHit: entries[i].CacheHit,
			Input:    entries[i].Input,
			Output:   entries[i].Output,
			Currency: entries[i].Currency,
		})
	}
	return entries
}

// LookupCatalog finds an official entry. billingMode empty matches payg first.
func LookupCatalog(provider, model, currency, billingMode string) (CatalogEntry, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.TrimSpace(model)
	currency = NormalizeCurrency(currency)
	billingMode = strings.TrimSpace(billingMode)
	if billingMode == "" {
		billingMode = BillingModePAYG
	}
	var fallback CatalogEntry
	foundFallback := false
	for _, e := range OfficialCatalog() {
		if e.Provider != provider || e.Model != model {
			continue
		}
		if NormalizeCurrency(e.Currency) != currency {
			continue
		}
		if e.BillingMode == billingMode {
			return e, true
		}
		if e.BillingMode == BillingModePAYG {
			fallback = e
			foundFallback = true
		}
	}
	if foundFallback {
		return fallback, true
	}
	return CatalogEntry{}, false
}

// RateCardFromCatalog builds a RateCard from a catalog entry's base rate.
func RateCardFromCatalog(e CatalogEntry) RateCard {
	return RateCard{
		CacheHit: e.CacheHit,
		Input:    e.Input,
		Output:   e.Output,
		Currency: e.Currency,
	}
}

// RatesAt is what this entry charges at t: the peak card inside its window, the
// base card everywhere else. An entry without a window charges one rate, and
// then t does not matter.
func (e CatalogEntry) RatesAt(t time.Time) RateCard {
	if e.Peak == nil || !e.Window.IsPeak(t) {
		return RateCardFromCatalog(e)
	}
	peak := *e.Peak
	peak.Currency = e.Currency
	return peak
}

// InEffect reports whether this entry's dates cover the day t falls on.
func (e CatalogEntry) InEffect(t time.Time) bool {
	day := t.UTC().Format("2006-01-02")
	if e.EffectiveFrom != "" && day < e.EffectiveFrom {
		return false
	}
	return e.EffectiveTo == "" || day < e.EffectiveTo
}

// MatchesCatalog reports whether rates equal a known official entry.
func MatchesCatalog(provider, model string, rates RateCard) (CatalogEntry, bool) {
	cur := NormalizeCurrency(rates.Currency)
	for _, e := range OfficialCatalog() {
		if e.Provider != strings.ToLower(strings.TrimSpace(provider)) {
			continue
		}
		if e.Model != strings.TrimSpace(model) {
			continue
		}
		if NormalizeCurrency(e.Currency) != cur {
			continue
		}
		// Either card identifies the vendor's own price. Which one a user
		// happens to have configured says nothing about when they are billing:
		// the hour decides that, and RatesAt is what reads it.
		if e.CacheHit == rates.CacheHit && e.Input == rates.Input && e.Output == rates.Output {
			return e, true
		}
		if p := e.Peak; p != nil && p.CacheHit == rates.CacheHit && p.Input == rates.Input && p.Output == rates.Output {
			return e, true
		}
	}
	return CatalogEntry{}, false
}
