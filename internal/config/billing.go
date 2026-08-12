package config

import (
	"fmt"
	"strings"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

// BillingConfig controls host-side cost display. It never changes provider
// list-price tables (those use ProviderEntry.BillingCurrency).
type BillingConfig struct {
	// DisplayCurrency is auto|CNY|USD. Empty equals auto.
	DisplayCurrency string `toml:"display_currency"`
}

// DisplayCurrencyPref returns the user preference: "" (auto), "CNY", or "USD".
func (c *Config) DisplayCurrencyPref() string {
	if c == nil {
		return ""
	}
	if v := normalizeDisplayCurrency(c.Billing.DisplayCurrency); v != "" {
		return v
	}
	// Legacy [desktop].currency continues to work until rewritten.
	return c.DesktopCurrency()
}

// ExplicitDisplayCurrency returns only a user-pinned display currency. Empty
// is intentional: auto is resolved by a quote/wallet presentation surface.
func (c *Config) ExplicitDisplayCurrency() string {
	return c.DisplayCurrencyPref()
}

// ResolveDisplayCurrency is retained for callers that need a compatibility
// name. Auto is no longer resolved from language, host region, or browser
// locale; it remains empty until a surface supplies a wallet hint.
func (c *Config) ResolveDisplayCurrency() string {
	return c.ExplicitDisplayCurrency()
}

func normalizeDisplayCurrency(currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "AUTO":
		return ""
	case "CNY", "RMB", "CNH":
		return "CNY"
	case "USD":
		return "USD"
	default:
		return ""
	}
}

// SetDisplayCurrency pins the global display currency preference. It does not
// rewrite provider billing currencies or official price tables.
func (c *Config) SetDisplayCurrency(currency string) error {
	if c == nil {
		return fmt.Errorf("nil config")
	}
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "", "AUTO":
		c.Billing.DisplayCurrency = ""
		c.Desktop.Currency = "" // keep legacy field in sync for older readers
	case "CNY", "RMB", "CNH":
		c.Billing.DisplayCurrency = "CNY"
		c.Desktop.Currency = "CNY"
	case "USD":
		c.Billing.DisplayCurrency = "USD"
		c.Desktop.Currency = "USD"
	default:
		return fmt.Errorf("display currency %q: must be auto|CNY|USD", currency)
	}
	return nil
}

// SetDesktopCurrency is retained for call-site compatibility. It now only
// changes the display currency preference and never rewrites official prices.
func (c *Config) SetDesktopCurrency(currency string) error {
	return c.SetDisplayCurrency(currency)
}

// ProviderBillingCurrency returns the frozen list-price currency for a provider.
func (e *ProviderEntry) ProviderBillingCurrency() string {
	if e == nil {
		return ""
	}
	if v := billing.NormalizeCurrency(e.BillingCurrency); v != "" {
		return v
	}
	// Infer from configured prices when field is absent (pre-migration).
	if e.Price != nil {
		if v := billing.NormalizeCurrency(e.Price.Currency); v != "" {
			return v
		}
	}
	for _, p := range e.Prices {
		if p == nil {
			continue
		}
		if v := billing.NormalizeCurrency(p.Currency); v != "" {
			return v
		}
	}
	return ""
}

// ProviderBillingMode returns payg or subscription_equivalent.
func (e *ProviderEntry) ProviderBillingMode() string {
	if e == nil {
		return billing.BillingModePAYG
	}
	switch strings.ToLower(strings.TrimSpace(e.BillingMode)) {
	case billing.BillingModeSubscriptionEquivalent, "subscription", "token_plan":
		return billing.BillingModeSubscriptionEquivalent
	default:
		return billing.BillingModePAYG
	}
}

// RateCardForModel builds a billing.RateCard for the active model price.
func (e *ProviderEntry) RateCardForModel(model string) billing.RateCard {
	p := e.PriceForModel(model)
	if p == nil {
		return billing.RateCard{Currency: e.ProviderBillingCurrency()}
	}
	cur := billing.NormalizeCurrency(p.Currency)
	if cur == "" {
		cur = e.ProviderBillingCurrency()
	}
	return billing.RateCard{
		CacheHit: p.CacheHit,
		Input:    p.Input,
		Output:   p.Output,
		Currency: cur,
	}
}

// freezeProviderBillingCurrencies sets BillingCurrency from current official
// prices when missing. Custom prices keep their currency. Never overwrites an
// explicit BillingCurrency.
func freezeProviderBillingCurrencies(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if strings.TrimSpace(p.BillingCurrency) != "" {
			p.BillingCurrency = billing.NormalizeCurrency(p.BillingCurrency)
			continue
		}
		if cur := p.ProviderBillingCurrency(); cur != "" {
			p.BillingCurrency = cur
		} else if officialProviderKind(p) == "deepseek" {
			// New default templates use USD official table historically.
			p.BillingCurrency = "USD"
		}
		if strings.TrimSpace(p.BillingMode) == "" {
			if isMiMoTokenPlanProvider(p) {
				p.BillingMode = billing.BillingModeSubscriptionEquivalent
			} else {
				p.BillingMode = billing.BillingModePAYG
			}
		}
	}
}

func isMiMoTokenPlanProvider(p *ProviderEntry) bool {
	if p == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(p.Name))
	preset := strings.ToLower(strings.TrimSpace(p.PresetID))
	return strings.Contains(name, "token-plan") || strings.Contains(preset, "token-plan")
}

// migrateBillingDisplayCurrency copies legacy desktop.currency into billing.
func migrateBillingDisplayCurrency(c *Config) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.Billing.DisplayCurrency) != "" {
		c.Billing.DisplayCurrency = normalizeDisplayCurrency(c.Billing.DisplayCurrency)
		return
	}
	if cur := c.DesktopCurrency(); cur != "" {
		c.Billing.DisplayCurrency = cur
	}
}

// DeepSeekOfficialPricingCurrency resolves which official DeepSeek table to
// use for *new default template fills*. It no longer follows display currency
// for existing providers — those use BillingCurrency. For templates without a
// frozen currency, prefer the first provider's billing currency, else USD.
func (c *Config) DeepSeekOfficialPricingCurrency() string {
	if c != nil {
		for i := range c.Providers {
			p := &c.Providers[i]
			if officialProviderKind(p) != "deepseek" {
				continue
			}
			if cur := p.ProviderBillingCurrency(); cur != "" {
				return cur
			}
		}
	}
	return "USD"
}

// QuoteForUsage builds a CostQuote from a provider price and usage tokens.
func QuoteForUsage(price *provider.Pricing, usage *provider.Usage, display string, modelRef, usageSource, billingMode, catalogSource string) billing.CostQuote {
	if price == nil || usage == nil {
		return billing.CostQuote{Estimated: true, CostComplete: false, DisplayComplete: false, Complete: false, DisplayStatus: billing.DisplayStatusUnavailable, IncompleteReason: "missing_price_or_usage"}
	}
	card := billing.RateCard{
		CacheHit: price.CacheHit,
		Input:    price.Input,
		Output:   price.Output,
		Currency: billing.NormalizeCurrency(price.Currency),
	}
	if card.Currency == "" {
		card.Currency = "CNY"
	}
	return billing.BuildQuote(billing.QuoteInput{
		Usage: billing.UsageTokens{
			PromptTokens:           usage.PromptTokens,
			CompletionTokens:       usage.CompletionTokens,
			CacheHitTokens:         usage.CacheHitTokens,
			CacheMissTokens:        usage.CacheMissTokens,
			CacheWriteTokens:       usage.CacheWriteTokens,
			CacheWriteBilledTokens: usage.CacheWriteBilledTokens,
			Estimated:              usage.Estimated,
		},
		Rates:           card,
		DisplayCurrency: display,
		BillingMode:     billingMode,
		ModelRef:        modelRef,
		UsageSource:     usageSource,
		CatalogSource:   catalogSource,
	})
}
