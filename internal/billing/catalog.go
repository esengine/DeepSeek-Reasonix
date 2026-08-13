package billing

import "strings"

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
}

// CatalogSourceURLs document public pricing pages.
const (
	DocDeepSeekPricing   = "https://api-docs.deepseek.com/quick_start/pricing"
	DocLongCatPricingUSD = "https://longcat.chat/platform/docs/Pricing/LongCat-2.0.html"
	DocLongCatPricingCNY = "https://longcat.chat/platform/docs/zh/pricing/long-cat-2.0"
	DocMiMoPAYG          = "https://mimo.mi.com/docs/price/pay-as-you-go"
	DocMiMoTokenPlan     = "https://platform.xiaomimimo.com/token-plan"
)

// OfficialCatalog is the built-in price book. Rates match config defaults.
func OfficialCatalog() []CatalogEntry {
	entries := []CatalogEntry{
		// DeepSeek regional tables (official pricing doc).
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-flash", Currency: "USD", CacheHit: 0.0028, Input: 0.14, Output: 0.28, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},
		{Provider: "deepseek", Model: "deepseek-v4-pro", Currency: "USD", CacheHit: 0.003625, Input: 0.435, Output: 0.87, DocURL: DocDeepSeekPricing, BillingMode: BillingModePAYG},

		// LongCat dual currency tables (official discounted list prices).
		// https://longcat.chat/platform/docs/Pricing/LongCat-2.0.html
		{Provider: "longcat", Model: "LongCat-2.0", Currency: "CNY", CacheHit: 0.04, Input: 2, Output: 8, DocURL: DocLongCatPricingCNY, BillingMode: BillingModePAYG},
		{Provider: "longcat", Model: "LongCat-2.0", Currency: "USD", CacheHit: 0.006, Input: 0.30, Output: 1.20, DocURL: DocLongCatPricingUSD, BillingMode: BillingModePAYG},

		// MiMo domestic PAYG; Token Plan uses the same rates as subscription_equivalent.
		{Provider: "mimo", Model: "mimo-v2.5-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2.5", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2-flash", Currency: "CNY", CacheHit: 0.07, Input: 0.70, Output: 2.10, DocURL: DocMiMoPAYG, BillingMode: BillingModePAYG},
		{Provider: "mimo", Model: "mimo-v2.5-pro", Currency: "CNY", CacheHit: 0.025, Input: 3, Output: 6, DocURL: DocMiMoTokenPlan, BillingMode: BillingModeSubscriptionEquivalent, Notes: "payg_equivalent_not_plan_bill"},
		{Provider: "mimo", Model: "mimo-v2.5", Currency: "CNY", CacheHit: 0.02, Input: 1, Output: 2, DocURL: DocMiMoTokenPlan, BillingMode: BillingModeSubscriptionEquivalent, Notes: "payg_equivalent_not_plan_bill"},
	}
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

// RateCardFromCatalog builds a RateCard from a catalog entry.
func RateCardFromCatalog(e CatalogEntry) RateCard {
	return RateCard{
		CacheHit: e.CacheHit,
		Input:    e.Input,
		Output:   e.Output,
		Currency: e.Currency,
	}
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
		if e.CacheHit == rates.CacheHit && e.Input == rates.Input && e.Output == rates.Output {
			return e, true
		}
	}
	return CatalogEntry{}, false
}
