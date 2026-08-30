package billing

import (
	"strings"
	"time"
)

const (
	ScheduleDeepSeekV4August2026 = "deepseek-v4-2026-08-17"
	RateBandPeak                 = "peak"
	RateBandOffPeak              = "off_peak"
	RateBandMixed                = "mixed"
)

var deepSeekV4August2026EffectiveAt = time.Date(2026, time.August, 16, 16, 0, 0, 0, time.UTC)

// CatalogEntry is one official list price for a model in a billing currency.
type CatalogEntry struct {
	Provider      string // deepseek | longcat | mimo
	Model         string
	Currency      string // ISO billing currency for this row
	CacheHit      float64
	Input         float64
	Output        float64
	ScheduleID    string
	RateBand      string // peak | off_peak; empty for static or legacy rows
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	DocURL        string
	BillingMode   string // payg | subscription_equivalent
	Notes         string
	Fingerprint   string // filled by OfficialCatalog
}

// ResolvedRate is the occurrence-time rate selected from an official schedule.
type ResolvedRate struct {
	Card       RateCard
	RateBand   string
	ScheduleID string
	OccurredAt time.Time
}

const (
	DocDeepSeekPricing   = "https://api-docs.deepseek.com/quick_start/pricing"
	DocLongCatPricingUSD = "https://longcat.chat/platform/docs/Pricing/LongCat-2.0.html"
	DocLongCatPricingCNY = "https://longcat.chat/platform/docs/zh/pricing/long-cat-2.0"
	DocMiMoPAYG          = "https://mimo.mi.com/docs/price/pay-as-you-go"
	DocMiMoTokenPlan     = "https://platform.xiaomimimo.com/token-plan"
)

// OfficialCatalog is the built-in price book. DeepSeek's scheduled rows use
// exact UTC instants so historical quote fixtures do not depend on host locale.
func OfficialCatalog() []CatalogEntry {
	cutover := deepSeekV4August2026EffectiveAt
	entries := []CatalogEntry{
		// Current DeepSeek V4 regional tables. Peak rows are the persisted config
		// anchors; occurrence-time resolution substitutes off-peak rows.
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "CNY", CacheHit: 0.10, Input: 3, Output: 9, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "CNY", CacheHit: 0.05, Input: 1.5, Output: 4.5, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "CNY", CacheHit: 0.10, Input: 3, Output: 9, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "CNY", CacheHit: 0.05, Input: 1.5, Output: 4.5, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "CNY", CacheHit: 0.30, Input: 9, Output: 27, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "CNY", CacheHit: 0.15, Input: 4.5, Output: 13.5, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "USD", CacheHit: 0.014, Input: 0.44, Output: 1.32, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "USD", CacheHit: 0.007, Input: 0.22, Output: 0.66, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "USD", CacheHit: 0.014, Input: 0.44, Output: 1.32, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "USD", CacheHit: 0.007, Input: 0.22, Output: 0.66, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "USD", CacheHit: 0.044, Input: 1.32, Output: 3.96, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "USD", CacheHit: 0.022, Input: 0.66, Output: 1.98, ScheduleID: ScheduleDeepSeekV4August2026, RateBand: RateBandOffPeak, EffectiveFrom: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},

		// Historical DeepSeek prices are available only to explicitly dated
		// schedule resolution. Persisted quotes are never repriced from these rows.
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "USD", CacheHit: 0.0028, Input: 0.14, Output: 0.28, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash-vision-exp", Currency: "USD", CacheHit: 0.0028, Input: 0.14, Output: 0.28, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "USD", CacheHit: 0.003625, Input: 0.435, Output: 0.87, ScheduleID: ScheduleDeepSeekV4August2026, EffectiveTo: cutover, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},

		{Provider: "longcat", Model: "LongCat-2.0", Currency: "CNY", CacheHit: 0.04, Input: 2, Output: 8, DocURL: DocLongCatPricingCNY, BillingMode: BillingModePAYG},
		{Provider: "longcat", Model: "LongCat-2.0", Currency: "USD", CacheHit: 0.006, Input: 0.30, Output: 1.20, DocURL: DocLongCatPricingUSD, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2.5-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2.5", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2-flash", Currency: "CNY", CacheHit: 0.07, Input: 0.70, Output: 2.10, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2.5-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, DocURL: DocMiMoTokenPlan, BillingMode: BillingModeSubscriptionEquivalent, Notes: "payg_equivalent_not_plan_bill"},
		{Provider: "mimo", Model: "mimo-v2.5", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, DocURL: DocMiMoTokenPlan, BillingMode: BillingModeSubscriptionEquivalent, Notes: "payg_equivalent_not_plan_bill"},
	}
	for i := range entries {
		entries[i].Fingerprint = PricingFingerprint(RateCardFromCatalog(entries[i]))
	}
	return entries
}

func normalizeBillingMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return BillingModePAYG
	}
	return strings.TrimSpace(mode)
}

func catalogIdentityMatches(e CatalogEntry, provider, model, currency, billingMode string) bool {
	return e.Provider == strings.ToLower(strings.TrimSpace(provider)) &&
		e.Model == strings.TrimSpace(model) &&
		NormalizeCurrency(e.Currency) == NormalizeCurrency(currency) &&
		e.BillingMode == normalizeBillingMode(billingMode)
}

func catalogEntryEffective(e CatalogEntry, at time.Time) bool {
	if !e.EffectiveFrom.IsZero() && at.Before(e.EffectiveFrom) {
		return false
	}
	return e.EffectiveTo.IsZero() || at.Before(e.EffectiveTo)
}

// DeepSeekRateBand selects the documented Beijing peak windows by their stable
// UTC equivalents.
func DeepSeekRateBand(at time.Time) string {
	at = at.UTC()
	minutes := at.Hour()*60 + at.Minute()
	if (minutes >= 60 && minutes < 240) || (minutes >= 360 && minutes < 600) {
		return RateBandPeak
	}
	return RateBandOffPeak
}

// ResolveScheduledRate resolves an official occurrence-time rate. The schedule
// id must come from resolved official-provider config; a model name is not enough.
func ResolveScheduledRate(provider, model, currency, billingMode, scheduleID string, at time.Time) (ResolvedRate, bool) {
	if strings.TrimSpace(scheduleID) == "" {
		return ResolvedRate{}, false
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	band := ""
	if scheduleID == ScheduleDeepSeekV4August2026 && !at.Before(deepSeekV4August2026EffectiveAt) {
		band = DeepSeekRateBand(at)
	}
	for _, e := range OfficialCatalog() {
		if e.ScheduleID != scheduleID || e.RateBand != band || !catalogEntryEffective(e, at) {
			continue
		}
		if catalogIdentityMatches(e, provider, model, currency, billingMode) {
			return ResolvedRate{Card: RateCardFromCatalog(e), RateBand: band, ScheduleID: scheduleID, OccurredAt: at}, true
		}
	}
	return ResolvedRate{}, false
}

// LookupCatalog finds the preferred current official entry. Scheduled models
// expose their peak row as the stable config anchor.
func LookupCatalog(provider, model, currency, billingMode string) (CatalogEntry, bool) {
	var fallback CatalogEntry
	for _, e := range OfficialCatalog() {
		if !catalogIdentityMatches(e, provider, model, currency, billingMode) {
			continue
		}
		if e.ScheduleID == "" || e.RateBand == RateBandPeak {
			return e, true
		}
		if fallback.Provider == "" {
			fallback = e
		}
	}
	return fallback, fallback.Provider != ""
}

// LookupCatalogAt returns the matching occurrence-time peer row used for an
// official dual-currency valuation.
func LookupCatalogAt(provider, model, currency, billingMode, scheduleID, rateBand string, at time.Time) (CatalogEntry, bool) {
	for _, e := range OfficialCatalog() {
		if e.ScheduleID != scheduleID || e.RateBand != rateBand || !catalogEntryEffective(e, at) {
			continue
		}
		if catalogIdentityMatches(e, provider, model, currency, billingMode) {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

func RateCardFromCatalog(e CatalogEntry) RateCard {
	return RateCard{CacheHit: e.CacheHit, Input: e.Input, Output: e.Output, Currency: e.Currency}
}

func MatchesCatalog(provider, model string, rates RateCard) (CatalogEntry, bool) {
	cur := NormalizeCurrency(rates.Currency)
	for _, e := range OfficialCatalog() {
		// Historical and off-peak rows require a trusted schedule resolution.
		// Static custom rates that merely equal one of those rows are not official.
		if e.ScheduleID != "" && e.RateBand != RateBandPeak {
			continue
		}
		if e.Provider != strings.ToLower(strings.TrimSpace(provider)) || e.Model != strings.TrimSpace(model) || NormalizeCurrency(e.Currency) != cur {
			continue
		}
		if e.CacheHit == rates.CacheHit && e.Input == rates.Input && e.Output == rates.Output {
			return e, true
		}
	}
	return CatalogEntry{}, false
}

// MatchesScheduleAnchor verifies that configured rates are the current peak
// anchor. Custom and off-peak-looking static prices stay static.
func MatchesScheduleAnchor(provider, model, scheduleID string, rates RateCard) bool {
	entry, ok := LookupCatalog(provider, model, rates.Currency, BillingModePAYG)
	return ok && entry.ScheduleID == scheduleID && entry.RateBand == RateBandPeak &&
		entry.CacheHit == rates.CacheHit && entry.Input == rates.Input && entry.Output == rates.Output
}
