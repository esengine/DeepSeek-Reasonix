package config

import (
	"fmt"
	"os"
	"strings"

	"reasonix/internal/provider"
)

func deepSeekV4FlashPriceCNY() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
}

func deepSeekV4ProPriceCNY() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}
}

func deepSeekV4PricesCNY() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": deepSeekV4FlashPriceCNY(),
		"deepseek-v4-pro":   deepSeekV4ProPriceCNY(),
	}
}

func deepSeekV4FlashPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.0028, Input: 0.14, Output: 0.28, Currency: "$"}
}

func deepSeekV4ProPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.003625, Input: 0.435, Output: 0.87, Currency: "$"}
}

func deepSeekV4PricesUSD() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": deepSeekV4FlashPriceUSD(),
		"deepseek-v4-pro":   deepSeekV4ProPriceUSD(),
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

// DeepSeekOfficialPricingCurrency resolves the regional pricing table used for
// official DeepSeek defaults. An explicit desktop currency wins; auto follows
// desktop language, then CLI language, and finally defaults to USD.
func (c *Config) DeepSeekOfficialPricingCurrency() string {
	if c != nil {
		if currency := c.DesktopCurrency(); currency != "" {
			return currency
		}
		if normalizeDeepSeekPricingLanguage(c.Desktop.Language) == "zh" {
			return "CNY"
		}
		if normalizeDeepSeekPricingLanguage(c.Desktop.Language) == "en" {
			return "USD"
		}
		if normalizeDeepSeekPricingLanguage(c.Language) == "zh" {
			return "CNY"
		}
	}
	return "USD"
}

// DesktopPricingFollowsDetectedLocale reports whether the desktop may supply
// its browser/OS locale as the official pricing region. Explicit currency or
// language preferences always win.
func (c *Config) DesktopPricingFollowsDetectedLocale() bool {
	return c != nil && c.DesktopCurrency() == "" && c.DesktopLanguage() == "" && c.ResponseLanguage() == "auto"
}

// ApplyRuntimeAutoPricingCurrency applies a frontend-detected pricing region
// to this in-memory config only. Callers must not persist the resulting copy,
// because doing so would turn the user's Auto choice into an explicit region.
func (c *Config) ApplyRuntimeAutoPricingCurrency(currency string) {
	if !c.DesktopPricingFollowsDetectedLocale() {
		return
	}
	normalized := normalizeDeepSeekPricingCurrency(currency)
	if normalized == "" {
		return
	}
	c.Desktop.Currency = normalized
	applyDeepSeekOfficialDefaultPricingWithOverride(c, false)
}

// DeepSeekOfficialPricingLanguage is retained for older settings/template call
// sites that still express the pricing region as a language.
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
// prices that still match known official defaults. Custom user prices are left
// untouched.
func (c *Config) ApplyDeepSeekOfficialDefaultPricing() {
	applyDeepSeekOfficialDefaultPricing(c)
}

func applyDeepSeekOfficialDefaultPricing(c *Config) {
	applyDeepSeekOfficialDefaultPricingWithOverride(c, c != nil && c.DesktopCurrency() != "")
}

func applyDeepSeekOfficialDefaultPricingWithOverride(c *Config, overridePersisted bool) {
	if c == nil {
		return
	}
	currency := c.DeepSeekOfficialPricingCurrency()
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		if isKnownDeepSeekOfficialPricing(p.Model, p.Price) && (overridePersisted || p.persistedOfficialCurrency == "") {
			p.Price = deepSeekV4PriceForModel(currency, p.Model)
		}
		for model, price := range p.Prices {
			if isKnownDeepSeekOfficialPricing(model, price) && (overridePersisted || p.persistedOfficialCurrency == "") {
				p.Prices[model] = deepSeekV4PriceForModel(currency, model)
			}
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
)

// ApplyUserConfigUpgradesOnStartup applies one-time startup migrations. It
// intentionally runs from the desktop and CLI startup paths, not every config
// Load(), so user edits made after the upgrade are preserved.
func ApplyUserConfigUpgradesOnStartup(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
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

func isKnownDeepSeekOfficialPricing(model string, price *provider.Pricing) bool {
	model = strings.TrimSpace(model)
	if model == "" || price == nil {
		return false
	}
	for _, prices := range []map[string]*provider.Pricing{deepSeekV4PricesCNY(), deepSeekV4PricesUSD()} {
		if samePricing(price, prices[model]) {
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
