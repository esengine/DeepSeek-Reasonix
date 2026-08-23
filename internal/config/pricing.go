package config

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

func deepSeekV4FlashPriceCNY() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.10, Input: 3, Output: 9, Currency: "¥"}
}

func deepSeekV4ProPriceCNY() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.30, Input: 9, Output: 27, Currency: "¥"}
}

func deepSeekV4PricesCNY() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash":                deepSeekV4FlashPriceCNY(),
		openai.OfficialDeepSeekVisionModel: deepSeekV4FlashPriceCNY(),
		"deepseek-v4-pro":                  deepSeekV4ProPriceCNY(),
	}
}

func deepSeekV4FlashPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.014, Input: 0.44, Output: 1.32, Currency: "$"}
}

func deepSeekV4ProPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.044, Input: 1.32, Output: 3.96, Currency: "$"}
}

func deepSeekV4PricesUSD() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash":                deepSeekV4FlashPriceUSD(),
		openai.OfficialDeepSeekVisionModel: deepSeekV4FlashPriceUSD(),
		"deepseek-v4-pro":                  deepSeekV4ProPriceUSD(),
	}
}

// DeepSeekV4PricesForCurrency returns the official regional price table.
// Persisted custom prices still win; this is only used for built-in templates
// and known-default refreshes.
func DeepSeekV4PricesForCurrency(currency string) map[string]*provider.Pricing {
	if normalizeDeepSeekPricingCurrency(currency) == "CNY" {
		return deepSeekV4PricesCNY()
	}
	return deepSeekV4PricesUSD()
}

// DeepSeekV4PricesForLanguage is retained for compatibility with older call
// sites. New desktop code should pass an explicit pricing currency.
func DeepSeekV4PricesForLanguage(lang string) map[string]*provider.Pricing {
	if normalizeDeepSeekPricingLanguage(lang) == "zh" {
		return DeepSeekV4PricesForCurrency("CNY")
	}
	return DeepSeekV4PricesForCurrency("USD")
}

func deepSeekV4PricesForConfig(c *Config) map[string]*provider.Pricing {
	return DeepSeekV4PricesForCurrency(c.DeepSeekOfficialPricingCurrency())
}

func deepSeekV4PriceForModel(currency, model string) *provider.Pricing {
	return clonePricing(DeepSeekV4PricesForCurrency(currency)[strings.TrimSpace(model)])
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

func normalizeDeepSeekPricingLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "zh", "zh-cn", "zh-hans", "cn", "chinese", "中文", "zh-tw", "zh-hant", "zh-hk", "zh-mo":
		return "zh"
	case "en", "en-us", "en-gb", "english":
		return "en"
	default:
		return ""
	}
}

// ApplyDeepSeekOfficialDefaultPricing refreshes built-in/official DeepSeek
// prices that still match known official defaults for each provider's frozen
// billing_currency. Custom user prices and display-currency switches never
// rewrite list prices.
func (c *Config) ApplyDeepSeekOfficialDefaultPricing() {
	applyDeepSeekOfficialDefaultPricing(c)
}

func applyDeepSeekOfficialDefaultPricing(c *Config) {
	applyDeepSeekOfficialDefaultPricingWithOverride(c, false)
}

func applyDeepSeekOfficialDefaultPricingWithOverride(c *Config, overridePersisted bool) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" || !isOfficialDeepSeekBillingEndpoint(p) {
			continue
		}
		currency := p.ProviderBillingCurrency()
		if currency == "" {
			currency = "USD"
		}
		// Only refresh when the row still matches a known official default in
		// the provider's own billing currency. Display currency must not win.
		if isKnownDeepSeekOfficialPricing(p.Model, p.Price) && (overridePersisted || p.persistedOfficialCurrency == "" || p.persistedOfficialCurrency == currency) {
			if samePricing(p.Price, deepSeekV4PriceForModel(currency, p.Model)) || overridePersisted {
				p.Price = deepSeekV4PriceForModel(currency, p.Model)
			}
		}
		for model, price := range p.Prices {
			if isKnownDeepSeekOfficialPricing(model, price) && (overridePersisted || p.persistedOfficialCurrency == "" || p.persistedOfficialCurrency == currency) {
				if samePricing(price, deepSeekV4PriceForModel(currency, model)) || overridePersisted {
					p.Prices[model] = deepSeekV4PriceForModel(currency, model)
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
		if officialProviderKind(p) != "deepseek" || !isOfficialDeepSeekBillingEndpoint(p) {
			continue
		}
		p.persistedOfficialCurrency = completeDeepSeekOfficialPricingCurrency(p)
		if c.ConfigVersion >= Default().ConfigVersion && isStandardDeepSeekProviderTemplate(p) {
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
	if len(models) == 1 && isKnownDeepSeekOfficialPricing(models[0], p.Price) {
		return normalizeDeepSeekPricingCurrency(p.Price.Currency)
	}
	if len(models) == 0 || p.Price != nil {
		return ""
	}
	currency := ""
	for _, model := range models {
		price := p.Prices[strings.TrimSpace(model)]
		if !isKnownDeepSeekOfficialPricing(model, price) {
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

func mimoV25ProPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}
}

func mimoV25Price() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
}

func mimoV2FlashPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.07, Input: 0.70, Output: 2.10, Currency: "¥"}
}

func mimoDomesticPrices(models []string) map[string]*provider.Pricing {
	prices := map[string]*provider.Pricing{}
	for _, model := range models {
		switch strings.TrimSpace(model) {
		case "mimo-v2.5-pro", "mimo-v2-pro":
			prices[model] = mimoV25ProPrice()
		case "mimo-v2.5", "mimo-v2-omni":
			prices[model] = mimoV25Price()
		case "mimo-v2-flash":
			prices[model] = mimoV2FlashPrice()
		}
	}
	return prices
}

func longCat20Price() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.04, Input: 2, Output: 8, Currency: "¥"}
}

func longCat20Prices(models []string) map[string]*provider.Pricing {
	prices := map[string]*provider.Pricing{}
	for _, model := range models {
		switch strings.TrimSpace(model) {
		case "LongCat-2.0":
			prices[model] = longCat20Price()
		}
	}
	return prices
}

const (
	deepSeekPricingResetConfigVersion      = 3
	windowsBashSandboxDefaultConfigVersion = 4
	retiredAutoPlanConfigVersion           = 5
	billingSplitConfigVersion              = 6
	deepSeekScheduledPricingConfigVersion  = 7
)

// ApplyUserConfigUpgradesOnStartup applies one-time startup migrations. It
// intentionally runs from the desktop and CLI startup paths, not every config
// Load(), so user edits made after the upgrade are preserved.
func ApplyUserConfigUpgradesOnStartup(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return false, err
	}
	defer unlock()

	_, exists, err := statConfigPath(path)
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
	if header.ConfigVersion >= Default().ConfigVersion {
		return false, nil
	}
	cfg := LoadForEdit(path)
	changed := false
	if header.ConfigVersion < deepSeekPricingResetConfigVersion {
		resetOfficialProviderPricingDefaults(cfg)
		changed = true
	}
	if shouldMarkWindowsBashSandboxDefaultUpgrade(header.ConfigVersion) {
		resetWindowsBashSandboxDefaultOnUpgrade(cfg)
		// Mark the Windows v4 migration even when the user was already on off,
		// so a later manual enforce choice is not treated as the old template default.
		changed = true
	}
	if header.ConfigVersion < retiredAutoPlanConfigVersion {
		normalizeRetiredAutoPlan(cfg)
		// Mark every older config as migrated even when Auto Plan was already off;
		// the v5 renderer removes both retired keys so older binaries also observe
		// the manual-only default after a downgrade.
		changed = true
	}
	if header.ConfigVersion < billingSplitConfigVersion {
		migrateBillingDisplayCurrency(cfg)
		freezeProviderBillingCurrencies(cfg)
		changed = true
	}
	if header.ConfigVersion < deepSeekScheduledPricingConfigVersion {
		migrateDeepSeekScheduledPricingDefaults(cfg)
		// Mark every older config, including custom-price configs, so their values
		// remain user-owned on later startups instead of being reconsidered.
		changed = true
	}
	if !changed {
		return false, nil
	}
	cfg.ConfigVersion = Default().ConfigVersion
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

// ResetOfficialProviderPricingOnUpgrade is retained for older call sites.
func ResetOfficialProviderPricingOnUpgrade(path string) (bool, error) {
	return ApplyUserConfigUpgradesOnStartup(path)
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
			resetDeepSeekOfficialPricing(p, deepSeekV4PricesForConfig(c))
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

func legacyDeepSeekV4PricesCNY() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": {CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
		"deepseek-v4-pro":   {CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"},
	}
}

func legacyDeepSeekV4PricesUSD() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": {CacheHit: 0.0028, Input: 0.14, Output: 0.28, Currency: "$"},
		"deepseek-v4-pro":   {CacheHit: 0.003625, Input: 0.435, Output: 0.87, Currency: "$"},
	}
}

// migrateDeepSeekScheduledPricingDefaults replaces only the exact pre-August
// official defaults. Custom endpoints and any edited numeric rate remain intact.
func migrateDeepSeekScheduledPricingDefaults(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" || !isOfficialDeepSeekBillingEndpoint(p) {
			continue
		}
		currency := p.ProviderBillingCurrency()
		if currency == "" {
			currency = "USD"
		}
		legacy := legacyDeepSeekV4PricesUSD()
		if normalizeDeepSeekPricingCurrency(currency) == "CNY" {
			legacy = legacyDeepSeekV4PricesCNY()
		}
		migrate := func(model string, price *provider.Pricing) *provider.Pricing {
			old := legacy[strings.TrimSpace(model)]
			if !samePricingNormalizedCurrency(price, old) {
				return price
			}
			return deepSeekV4PriceForModel(currency, model)
		}
		if p.Price != nil {
			p.Price = migrate(p.Model, p.Price)
		}
		// Treat a multi-model official table atomically. A mixture of an old
		// default row and a user-edited row is custom as a whole; partially
		// rewriting it would leave an incoherent price book.
		canMigrateTable := false
		for model, price := range p.Prices {
			old, known := legacy[strings.TrimSpace(model)]
			if !known {
				continue
			}
			canMigrateTable = true
			if !samePricingNormalizedCurrency(price, old) {
				canMigrateTable = false
				break
			}
		}
		if canMigrateTable {
			for model, price := range p.Prices {
				p.Prices[model] = migrate(model, price)
			}
		}
	}
}

func isKnownDeepSeekOfficialPricing(model string, price *provider.Pricing) bool {
	model = strings.TrimSpace(model)
	if model == "" || price == nil {
		return false
	}
	for _, prices := range []map[string]*provider.Pricing{
		deepSeekV4PricesCNY(), deepSeekV4PricesUSD(), legacyDeepSeekV4PricesCNY(), legacyDeepSeekV4PricesUSD(),
	} {
		if samePricingNormalizedCurrency(price, prices[model]) {
			return true
		}
	}
	return false
}

// IsOfficialDeepSeekProvider reports whether an entry targets DeepSeek's
// official API endpoint. Desktop telemetry uses this after a regional-currency
// change so custom endpoints and rates stay untouched.
func IsOfficialDeepSeekProvider(p *ProviderEntry) bool {
	return officialProviderKind(p) == "deepseek"
}

// IsKnownDeepSeekOfficialPricing reports whether price is one of Reasonix's
// built-in DeepSeek regional defaults for model.
func IsKnownDeepSeekOfficialPricing(model string, price *provider.Pricing) bool {
	return isKnownDeepSeekOfficialPricing(model, price)
}

func samePricing(a, b *provider.Pricing) bool {
	if a == nil || b == nil {
		return false
	}
	return a.CacheHit == b.CacheHit && a.Input == b.Input && a.Output == b.Output && a.Currency == b.Currency
}

func samePricingNormalizedCurrency(a, b *provider.Pricing) bool {
	if a == nil || b == nil {
		return false
	}
	return a.CacheHit == b.CacheHit && a.Input == b.Input && a.Output == b.Output &&
		normalizeDeepSeekPricingCurrency(a.Currency) == normalizeDeepSeekPricingCurrency(b.Currency)
}
