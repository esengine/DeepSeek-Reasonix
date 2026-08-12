package doctor

import (
	"fmt"
	"strings"
	"time"

	"reasonix/internal/billing"
	"reasonix/internal/config"
)

// BillingReport is the structured output of `reasonix doctor billing`.
type BillingReport struct {
	DisplayCurrencyPref string                `json:"display_currency_pref"`
	DisplayCurrency     string                `json:"display_currency"`
	FX                  FXReport              `json:"fx"`
	Providers           []ProviderBillingInfo `json:"providers"`
	CatalogNotes        []string              `json:"catalog_notes,omitempty"`
}

// FXReport is retained for JSON compatibility. Runtime FX is intentionally
// disabled; old readers may continue to expect the object.
type FXReport struct {
	Enabled     bool      `json:"enabled"`
	Source      string    `json:"source"`
	CachePath   string    `json:"cache_path,omitempty"`
	FetchedAt   time.Time `json:"fetched_at,omitempty"`
	Stale       bool      `json:"stale"`
	HasTable    bool      `json:"has_table"`
	Observation string    `json:"latest_observation,omitempty"`
}

// ProviderBillingInfo describes one provider's frozen list-price currency.
type ProviderBillingInfo struct {
	Name            string  `json:"name"`
	Model           string  `json:"model,omitempty"`
	BillingCurrency string  `json:"billing_currency,omitempty"`
	BillingMode     string  `json:"billing_mode,omitempty"`
	PriceCurrency   string  `json:"price_currency,omitempty"`
	PriceInput      float64 `json:"price_input,omitempty"`
	PriceOutput     float64 `json:"price_output,omitempty"`
	PriceCacheHit   float64 `json:"price_cache_hit,omitempty"`
	CatalogMatch    bool    `json:"catalog_match"`
	CatalogSource   string  `json:"catalog_source,omitempty"`
	Fingerprint     string  `json:"pricing_fingerprint,omitempty"`
	CustomPrice     bool    `json:"custom_price"`
}

// CollectBilling builds a billing diagnostics report.
func CollectBilling(cfg *config.Config) BillingReport {
	if cfg == nil {
		cfg = config.Default()
	}
	rep := BillingReport{
		DisplayCurrencyPref: prefLabel(cfg.DisplayCurrencyPref()),
		DisplayCurrency:     cfg.ExplicitDisplayCurrency(),
		FX: FXReport{
			Enabled: false,
			Source:  "disabled",
		},
	}

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		info := ProviderBillingInfo{
			Name:            p.Name,
			Model:           p.Model,
			BillingCurrency: p.ProviderBillingCurrency(),
			BillingMode:     p.ProviderBillingMode(),
		}
		price := p.PriceForModel(p.Model)
		if price != nil {
			info.PriceCurrency = billing.NormalizeCurrency(price.Currency)
			info.PriceInput = price.Input
			info.PriceOutput = price.Output
			info.PriceCacheHit = price.CacheHit
			card := billing.RateCard{
				CacheHit: price.CacheHit, Input: price.Input, Output: price.Output,
				Currency: info.PriceCurrency,
			}
			info.Fingerprint = billing.PricingFingerprint(card)
			providerKind := officialKindForBilling(p)
			if entry, ok := billing.MatchesCatalog(providerKind, p.Model, card); ok {
				info.CatalogMatch = true
				info.CatalogSource = entry.DocURL
			} else if price != nil {
				info.CustomPrice = !info.CatalogMatch
			}
		}
		rep.Providers = append(rep.Providers, info)
	}
	rep.CatalogNotes = []string{
		"User-custom prices always win over the official catalog.",
		"MiMo Token Plan costs are pay-as-you-go equivalents, not plan invoices.",
		"Runtime FX is disabled; only identity and official regional rate-card estimates are used.",
		"Switching display_currency never rewrites provider billing_currency or list prices.",
	}
	return rep
}

func prefLabel(pref string) string {
	if pref == "" {
		return "auto"
	}
	return pref
}

func officialKindForBilling(p *config.ProviderEntry) string {
	if p == nil {
		return ""
	}
	name := strings.ToLower(p.Name)
	base := strings.ToLower(p.BaseURL)
	switch {
	case strings.Contains(base, "deepseek") || strings.Contains(name, "deepseek"):
		return "deepseek"
	case strings.Contains(base, "longcat") || strings.Contains(name, "longcat"):
		return "longcat"
	case strings.Contains(base, "mimo") || strings.Contains(name, "mimo"):
		return "mimo"
	default:
		return name
	}
}

// RenderBillingText formats a human-readable billing doctor report.
func RenderBillingText(r BillingReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reasonix doctor billing\n")
	fmt.Fprintf(&b, "  display preference: %s\n", r.DisplayCurrencyPref)
	fmt.Fprintf(&b, "  display resolved:   %s\n", r.DisplayCurrency)
	fmt.Fprintf(&b, "  fx source:          %s\n", r.FX.Source)
	fmt.Fprintf(&b, "  fx enabled:         %v\n", r.FX.Enabled)
	fmt.Fprintf(&b, "  auto strategy:      wallet currency, then pricing basis\n")
	b.WriteString("\n  providers:\n")
	for _, p := range r.Providers {
		fmt.Fprintf(&b, "    - %s model=%s billing=%s mode=%s", p.Name, p.Model, p.BillingCurrency, p.BillingMode)
		if p.PriceCurrency != "" {
			fmt.Fprintf(&b, " price=%s/%.4g/%.4g", p.PriceCurrency, p.PriceInput, p.PriceOutput)
		}
		if p.CatalogMatch {
			fmt.Fprintf(&b, " [official: %s]", p.CatalogSource)
		} else if p.CustomPrice {
			b.WriteString(" [custom price protected]")
		}
		b.WriteByte('\n')
		if p.Fingerprint != "" {
			fmt.Fprintf(&b, "      fingerprint=%s\n", p.Fingerprint)
		}
	}
	if len(r.CatalogNotes) > 0 {
		b.WriteString("\n  notes:\n")
		for _, n := range r.CatalogNotes {
			fmt.Fprintf(&b, "    - %s\n", n)
		}
	}
	return b.String()
}
