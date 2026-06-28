package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/provider"
)

func deepSeekV4FlashPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
}

func deepSeekV4ProPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}
}

func deepSeekV4Prices() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": deepSeekV4FlashPrice(),
		"deepseek-v4-pro":   deepSeekV4ProPrice(),
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

// DeepSeekV4PricesForLanguage keeps the settings/template call site stable while
// official DeepSeek defaults move to RMB. Persisted prices still win; this is
// only used for templates and missing-default backfills.
func DeepSeekV4PricesForLanguage(lang string) map[string]*provider.Pricing {
	_ = lang
	return deepSeekV4Prices()
}

func deepSeekV4PricesForConfig(c *Config) map[string]*provider.Pricing {
	_ = c
	return deepSeekV4Prices()
}

// DeepSeekOfficialPricingLanguage is retained for settings/template compatibility.
// Official DeepSeek providers now seed RMB prices by default; explicit user
// prices in config still override these defaults.
func (c *Config) DeepSeekOfficialPricingLanguage() string {
	_ = c
	return "zh"
}

// ApplyDeepSeekOfficialDefaultPricing refreshes built-in/official DeepSeek
// prices that still match known official defaults. Custom user prices are left
// untouched.
func (c *Config) ApplyDeepSeekOfficialDefaultPricing() {
	applyDeepSeekOfficialDefaultPricing(c)
}

func applyDeepSeekOfficialDefaultPricing(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		if table := matchDeepSeekPriceTable(p.Price); table != nil {
			if updated := table[strings.TrimSpace(p.Model)]; updated != nil {
				p.Price = clonePricing(updated)
			}
		}
		for model, price := range p.Prices {
			if table := matchDeepSeekPriceTable(price); table != nil {
				if updated := table[strings.TrimSpace(model)]; updated != nil {
					p.Prices[model] = clonePricing(updated)
				}
			}
		}
	}
}

// matchDeepSeekPriceTable returns the DeepSeek price table that the given
// pricing matches, so callers can pull updates from the same table (preserving
// the user's currency choice). Returns nil when the pricing is custom. (#4814)
func matchDeepSeekPriceTable(price *provider.Pricing) map[string]*provider.Pricing {
	if price == nil {
		return nil
	}
	if p := deepSeekV4Prices(); isKnownInTable(price, p) {
		return p
	}
	if p := deepSeekV4PricesUSD(); isKnownInTable(price, p) {
		return p
	}
	return nil
}

func isKnownInTable(price *provider.Pricing, table map[string]*provider.Pricing) bool {
	for _, ref := range table {
		if samePricing(price, ref) {
			return true
		}
	}
	return false
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

// ResetOfficialProviderPricingOnUpgrade resets official DeepSeek prices to
// the current built-in RMB defaults once for desktop upgrades. It intentionally
// runs from the desktop app startup path, not every config Load(), so user edits
// made after the upgrade are preserved.
func ResetOfficialProviderPricingOnUpgrade(path string) (bool, error) {
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
	if _, err := toml.DecodeFile(path, &header); err != nil {
		return false, fmt.Errorf("config %s: %w", path, err)
	}
	if header.ConfigVersion >= Default().ConfigVersion {
		return false, nil
	}
	cfg := LoadForEdit(path)
	resetOfficialProviderPricingDefaults(cfg)
	cfg.ConfigVersion = Default().ConfigVersion
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func resetOfficialProviderPricingDefaults(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		switch {
		case officialProviderKind(p) == "deepseek":
			resetDeepSeekOfficialPricing(p)
		}
	}
}

func resetDeepSeekOfficialPricing(p *ProviderEntry) {
	if p == nil {
		return
	}
	defaults := deepSeekV4Prices()
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

func samePricing(a, b *provider.Pricing) bool {
	if a == nil || b == nil {
		return false
	}
	return a.CacheHit == b.CacheHit && a.Input == b.Input && a.Output == b.Output && a.Currency == b.Currency
}
