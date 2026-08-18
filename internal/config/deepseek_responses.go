package config

import (
	"strings"

	"reasonix/internal/provider"
)

func deepSeekV4EffortOverrides() map[string]ProviderModelOverride {
	return map[string]ProviderModelOverride{
		"deepseek-v4-flash": {SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "high"},
		"deepseek-v4-pro":   {SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "high"},
	}
}

func isOfficialDeepSeekResponsesProvider(p *ProviderEntry) bool {
	return p != nil && strings.EqualFold(strings.TrimSpace(p.Kind), "responses") &&
		officialProviderHost(p.BaseURL) == "api.deepseek.com"
}

// backfillOfficialDeepSeekResponsesModels upgrades the previous canned
// Flash-only official Responses contract. Settings drops overrides for
// unchecked models, so any leftover ModelOverrides means the list is curated.
func backfillOfficialDeepSeekResponsesModels(p *ProviderEntry) {
	if !isOfficialDeepSeekResponsesProvider(p) {
		return
	}
	switch strings.TrimSpace(p.Name) {
	case "deepseek", "deepseek-responses":
	default:
		return
	}
	if p.HasModel("deepseek-v4-pro") {
		backfillOfficialDeepSeekResponsesProPrice(p)
		return
	}
	models := p.ModelList()
	if len(models) != 1 || models[0] != "deepseek-v4-flash" {
		return
	}
	if len(p.ModelOverrides) > 0 {
		return
	}
	p.Models = mergeModelLists(models, []string{"deepseek-v4-flash", "deepseek-v4-pro"})
	p.Default = firstKnownModel(p.Default, p.Models, "deepseek-v4-flash")
	backfillOfficialDeepSeekResponsesProPrice(p)
}

func backfillOfficialDeepSeekResponsesCapabilities(p *ProviderEntry) {
	if !isOfficialDeepSeekResponsesProvider(p) {
		return
	}
	backfillOfficialDeepSeekEffortOverrides(p)
}

func backfillOfficialDeepSeekEffortOverrides(p *ProviderEntry) {
	if p == nil {
		return
	}
	// A provider-level vocabulary is user-owned and remains the fallback for
	// every model without an explicit per-model override. Do not shadow it with
	// generated model defaults on multi-model providers.
	if len(p.SupportedEfforts) > 0 {
		return
	}
	capabilities := deepSeekV4EffortOverrides()
	if model := strings.TrimSpace(p.Model); model != "" && len(p.Models) == 0 {
		defaults, ok := capabilities[model]
		if !ok {
			return
		}
		p.SupportedEfforts = append([]string(nil), defaults.SupportedEfforts...)
		if strings.TrimSpace(p.DefaultEffort) == "" {
			p.DefaultEffort = defaults.DefaultEffort
		}
		return
	}
	if p.ModelOverrides == nil {
		p.ModelOverrides = map[string]ProviderModelOverride{}
	}
	for model, defaults := range capabilities {
		if !p.HasModel(model) {
			continue
		}
		override := p.ModelOverrides[model]
		if len(override.SupportedEfforts) == 0 {
			override.SupportedEfforts = append([]string(nil), defaults.SupportedEfforts...)
			if strings.TrimSpace(override.DefaultEffort) == "" {
				override.DefaultEffort = defaults.DefaultEffort
			}
		}
		p.ModelOverrides[model] = override
	}
}

// backfillOfficialDeepSeekResponsesProPrice fills Prices[pro] when a legacy
// singular Price would otherwise make Pro inherit the Flash list price.
func backfillOfficialDeepSeekResponsesProPrice(p *ProviderEntry) {
	if p == nil || !p.HasModel("deepseek-v4-pro") || p.Prices["deepseek-v4-pro"] != nil {
		return
	}
	currency := p.ProviderBillingCurrency()
	if currency == "" {
		currency = "USD"
	}
	price := deepSeekV4PriceForModel(currency, "deepseek-v4-pro")
	if price == nil {
		return
	}
	if p.Prices == nil {
		p.Prices = map[string]*provider.Pricing{}
	}
	p.Prices["deepseek-v4-pro"] = price
}
