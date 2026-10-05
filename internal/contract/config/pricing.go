package config

import (
	"fmt"
	"reasonix/internal/contract/pricing"
	"strings"

	"reasonix/internal/contract/provider"
)

// DeepSeek's rates live in billing's history, which publishes the current
// generation and keeps the superseded ones. Reading them from there rather than
// restating them means a price change is one append in one place.
func deepSeekOfficialRate(model, currency string) *provider.Pricing {
	return officialVendorPrice("deepseek", currency, model)
}

func pricingFromRateCard(card *pricing.RateCard) *provider.Pricing {
	if card == nil {
		return nil
	}
	return &provider.Pricing{
		CacheHit: card.CacheHit, Input: card.Input, Output: card.Output,
		Currency: pricing.CurrencySymbol(card.Currency),
	}
}

func deepSeekOfficialPricesCNY() map[string]*provider.Pricing {
	return deepSeekOfficialRates("CNY")
}

func deepSeekOfficialPricesUSD() map[string]*provider.Pricing {
	return deepSeekOfficialRates("USD")
}

// deepSeekOfficialRates is what a connection to the official endpoint is offered.
// The retired names are priced too but not listed: a new entry seeded with one
// would be offering a model nobody should pick.
func deepSeekOfficialRates(currency string) map[string]*provider.Pricing {
	return officialVendorPrices("deepseek", currency, []string{DeepSeekFlashModel, deepSeekProModel})
}

// DeepSeekOfficialPricesForCurrency returns the official regional price table.
// Persisted custom prices still win; this is only used for built-in templates
// and known-default refreshes.
func DeepSeekOfficialPricesForCurrency(currency string) map[string]*provider.Pricing {
	if normalizeDeepSeekPricingCurrency(currency) == "CNY" {
		return deepSeekOfficialPricesCNY()
	}
	return deepSeekOfficialPricesUSD()
}

func deepSeekOfficialPricesForConfig(c *Config) map[string]*provider.Pricing {
	return DeepSeekOfficialPricesForCurrency(c.DeepSeekOfficialPricingCurrency())
}

func deepSeekOfficialPriceForModel(currency, model string) *provider.Pricing {
	return clonePricing(DeepSeekOfficialPricesForCurrency(currency)[pricing.RateModelFor("deepseek", model)])
}

// DeepSeekOfficialPricingLanguage is retained for older settings/template call
// sites that still express the pricing region as a language. List-price region
// is frozen per provider (billing_currency), not the global display currency.
func (c *Config) DeepSeekOfficialPricingLanguage() string {
	if c.DeepSeekOfficialPricingCurrency() == "CNY" {
		return "zh"
	}
	return "en"
}

func normalizeDeepSeekPricingCurrency(currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "CNY", "RMB", "CNH", "¥", "￥":
		return "CNY"
	case "USD", "$", "US$":
		return "USD"
	default:
		return ""
	}
}

// ApplyOfficialDefaultPricing refreshes built-in/official vendor
// prices that still match known official defaults for each provider's frozen
// billing_currency. Custom user prices and display-currency switches never
// rewrite list prices.
func (c *Config) ApplyOfficialDefaultPricing() {
	applyOfficialDefaultPricing(c)
}

func applyOfficialDefaultPricing(c *Config) {
	applyOfficialDefaultPricingWithOverride(c, false)
}

func applyOfficialDefaultPricingWithOverride(c *Config, overridePersisted bool) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		vendor := officialProviderKind(p)
		if vendor == "" {
			continue
		}
		currency := p.ProviderBillingCurrency()
		if currency == "" {
			currency = pricing.DefaultCurrency(vendor)
		}
		// Only refresh when the row still matches a known official default in
		// the provider's own billing currency. Display currency must not win.
		if isKnownOfficialPricing(vendor, p.Model, p.Price) && (overridePersisted || p.persistedOfficialCurrency == "" || p.persistedOfficialCurrency == currency) {
			switch refreshed := supersededOfficialRefresh(vendor, p.Model, p.Price); {
			case samePricing(p.Price, officialVendorPrice(vendor, currency, p.Model)) || overridePersisted:
				p.Price = officialVendorPrice(vendor, currency, p.Model)
			case refreshed != nil:
				p.Price = refreshed
			}
		}
		for model, price := range p.Prices {
			if isKnownOfficialPricing(vendor, model, price) && (overridePersisted || p.persistedOfficialCurrency == "" || p.persistedOfficialCurrency == currency) {
				switch refreshed := supersededOfficialRefresh(vendor, model, price); {
				case samePricing(price, officialVendorPrice(vendor, currency, model)) || overridePersisted:
					p.Prices[model] = officialVendorPrice(vendor, currency, model)
				case refreshed != nil:
					p.Prices[model] = refreshed
				}
			}
		}
		if strings.TrimSpace(p.BillingCurrency) == "" {
			p.BillingCurrency = currency
		}
	}
}

// markPersistedDeepSeekOfficialPricing records which recognized regional
// prices came from TOML. Auto locale refreshes preserve those values, while an
// explicit currency choice can still replace them with the selected table.
func markPersistedDeepSeekOfficialPricing(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		p.persistedOfficialCurrency = completeDeepSeekOfficialPricingCurrency(p)
		if c.ConfigVersion >= lastUpgradeConfigVersion && isStandardDeepSeekProviderTemplate(p) {
			p.persistedOfficialCurrency = ""
		}
	}
}

func isStandardDeepSeekProviderTemplate(p *ProviderEntry) bool {
	if p == nil || officialProviderKind(p) != "deepseek" {
		return false
	}
	return strings.TrimSpace(p.APIKeyEnv) == "DEEPSEEK_API_KEY" &&
		strings.TrimSpace(p.BalanceURL) == "https://api.deepseek.com/user/balance" &&
		p.ContextWindow == 1_000_000
}

func completeDeepSeekOfficialPricingCurrency(p *ProviderEntry) string {
	if p == nil {
		return ""
	}
	models := p.ModelList()
	if len(models) == 1 && isKnownOfficialPricing("deepseek", models[0], p.Price) {
		return normalizeDeepSeekPricingCurrency(p.Price.Currency)
	}
	if len(models) == 0 || p.Price != nil {
		return ""
	}
	currency := ""
	for _, model := range models {
		price := p.Prices[strings.TrimSpace(model)]
		if !isKnownOfficialPricing("deepseek", model, price) {
			return ""
		}
		nextCurrency := normalizeDeepSeekPricingCurrency(price.Currency)
		if nextCurrency == "" {
			return ""
		}
		if currency == "" {
			currency = nextCurrency
		} else if currency != nextCurrency {
			return ""
		}
	}
	return currency
}

// officialVendorPrices is a vendor's current rate for each model it prices.
// One reader for every vendor: the rates live in billing's history, and a table
// restated here is one that drifts from it the first time either side moves.
func officialVendorPrices(vendor, currency string, models []string) map[string]*provider.Pricing {
	prices := make(map[string]*provider.Pricing, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if rate := officialVendorPrice(vendor, currency, model); rate != nil {
			prices[model] = rate
		}
	}
	return prices
}

func officialVendorPrice(vendor, currency, model string) *provider.Pricing {
	return pricingFromRateCard(pricing.CurrentRate(vendor, model, currency))
}

func mimoDomesticPrices(models []string) map[string]*provider.Pricing {
	return officialVendorPrices("mimo", "CNY", models)
}

func longCat20Prices(models []string) map[string]*provider.Pricing {
	return officialVendorPrices("longcat", "CNY", models)
}

const (
	deepSeekPricingResetConfigVersion      = 3
	windowsBashSandboxDefaultConfigVersion = 4
	retiredAutoPlanConfigVersion           = 5
	billingSplitConfigVersion              = 6
	// lastUpgradeConfigVersion is the newest upgrade this build applies.
	lastUpgradeConfigVersion = billingSplitConfigVersion
	// freshConfigVersion marks a file this build writes from nothing. The 1.x
	// line shares the file and rewrites it whole, dropping every table it does
	// not know, when the marker is below its own latest upgrade, 12.
	freshConfigVersion = 12
)

// ApplyUserConfigUpgradesOnStartup applies one-time startup migrations. It
// intentionally runs from the desktop and CLI startup paths, not every config
// Load(), so user edits made after the upgrade are preserved.
func ApplyUserConfigUpgradesOnStartup(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	_, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return false, err
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return false, err
	}
	defer unlock()

	_, exists, err = statConfigPath(path)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	var header Config
	if _, err := decodeTOMLFile(path, &header); err != nil {
		return false, fmt.Errorf("config %s: %w", path, err)
	}
	upgraded, err := applyVersionedUpgrades(path, header.ConfigVersion)
	if err != nil {
		return upgraded, err
	}
	released, err := releaseShippedDesktopPosture(path)
	return upgraded || released, err
}

func applyVersionedUpgrades(path string, from int) (bool, error) {
	if from >= lastUpgradeConfigVersion {
		return false, nil
	}
	cfg := LoadForEdit(path)
	if from < deepSeekPricingResetConfigVersion {
		resetOfficialProviderPricingDefaults(cfg)
	}
	// Marked even when the user was already on off, so a later manual enforce
	// choice is not treated as the old template default.
	if shouldMarkWindowsBashSandboxDefaultUpgrade(from) {
		resetWindowsBashSandboxDefaultOnUpgrade(cfg)
	}
	// The v5 renderer removes both retired keys, so older binaries also observe
	// the manual-only default after a downgrade.
	if from < retiredAutoPlanConfigVersion {
		normalizeRetiredAutoPlan(cfg)
	}
	if from < billingSplitConfigVersion {
		migrateBillingDisplayCurrency(cfg)
		freezeProviderBillingCurrencies(cfg)
	}
	cfg.ConfigVersion = lastUpgradeConfigVersion
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func shouldMarkWindowsBashSandboxDefaultUpgrade(fromVersion int) bool {
	return runtimeGOOS == "windows" && fromVersion < windowsBashSandboxDefaultConfigVersion
}

func resetWindowsBashSandboxDefaultOnUpgrade(c *Config) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Sandbox.Bash) != "enforce" {
		return
	}
	c.Sandbox.Bash = "off"
}

func resetOfficialProviderPricingDefaults(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		switch {
		case officialProviderKind(p) == "deepseek":
			resetDeepSeekOfficialPricing(p, deepSeekOfficialPricesForConfig(c))
		}
	}
}

func resetDeepSeekOfficialPricing(p *ProviderEntry, defaults map[string]*provider.Pricing) {
	if p == nil {
		return
	}
	p.Price = nil
	if strings.TrimSpace(p.Model) != "" && len(p.Models) == 0 {
		if price := defaults[strings.TrimSpace(p.Model)]; price != nil {
			p.Price = clonePricing(price)
			p.Prices = nil
			return
		}
	}
	if p.Prices == nil {
		p.Prices = map[string]*provider.Pricing{}
	}
	for model, price := range defaults {
		if p.HasModel(model) {
			p.Prices[model] = clonePricing(price)
		}
	}
}

func isKnownOfficialPricing(vendor, model string, price *provider.Pricing) bool {
	model = strings.TrimSpace(model)
	if model == "" || price == nil {
		return false
	}
	if samePricing(price, officialVendorPrice(vendor, price.Currency, model)) {
		return true
	}
	return supersededOfficialRefresh(vendor, model, price) != nil
}

// supersededOfficialRefresh is the current rate for a stored price this project
// shipped before, in that price's own currency. The currency comes from the
// price rather than from the entry: an entry that never declared one falls back
// to the vendor's default, and a rate quoted in the other one would go
// unrecognised and never be updated.
func supersededOfficialRefresh(vendor, model string, price *provider.Pricing) *provider.Pricing {
	if price == nil {
		return nil
	}
	for _, card := range pricing.SupersededRates(vendor, model, price.Currency) {
		if samePricing(price, pricingFromRateCard(&card)) {
			return officialVendorPrice(vendor, price.Currency, model)
		}
	}
	return nil
}

// IsKnownOfficialPricing reports whether price is one of Reasonix's built-in
// defaults for a vendor's model — the current rate or one it has superseded.
func IsKnownOfficialPricing(vendor, model string, price *provider.Pricing) bool {
	return isKnownOfficialPricing(vendor, model, price)
}

func samePricing(a, b *provider.Pricing) bool {
	if a == nil || b == nil {
		return false
	}
	return a.CacheHit == b.CacheHit && a.Input == b.Input && a.Output == b.Output && a.Currency == b.Currency
}

func backfillDeepSeekOfficialPrices(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		backfillDeepSeekOfficialEndpointDefaults(p)
		currency := p.ProviderBillingCurrency()
		if currency == "" {
			currency = p.persistedOfficialCurrency
		}
		if currency == "" {
			currency = "USD"
		}
		if p.Price != nil {
			continue
		}
		if p.Prices == nil {
			p.Prices = map[string]*provider.Pricing{}
		}
		// From the entry's list, not the table's: a retired name is priced too.
		for _, model := range p.ModelList() {
			if price := deepSeekOfficialPriceForModel(currency, model); price != nil && p.Prices[model] == nil {
				p.Prices[model] = price
			}
		}
	}
}
