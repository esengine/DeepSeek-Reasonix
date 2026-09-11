package config

import (
	"slices"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

// OfficialDeepSeekPinnedVisionModels returns the official vision SKU when it is
// in the enabled model list. Settings can still mark other models for image
// input; this helper is only the stock default.
func OfficialDeepSeekPinnedVisionModels(models []string) []string {
	if !slices.ContainsFunc(models, openai.IsOfficialDeepSeekVisionModel) {
		return nil
	}
	return []string{openai.OfficialDeepSeekVisionModel}
}

func backfillOfficialDeepSeekVisionModel(p *ProviderEntry) {
	if p == nil || officialProviderHost(p.BaseURL) != "api.deepseek.com" {
		return
	}
	sku := openai.OfficialDeepSeekVisionModel
	models := p.ModelList()
	var stock []string
	switch strings.TrimSpace(p.Name) {
	case "deepseek-pro":
		return
	case "deepseek-flash":
		if officialDeepSeekModelSetEquals(models, []string{"deepseek-v4-flash", sku}) {
			stock = []string{"deepseek-v4-flash", sku}
		} else if officialDeepSeekModelSetEquals(models, []string{"deepseek-v4-flash"}) && p.VisionModels == nil {
			stock = []string{"deepseek-v4-flash", sku}
		}
	case "deepseek", "deepseek-anthropic", "deepseek-responses":
		if officialDeepSeekModelSetEquals(models, []string{"deepseek-v4-flash", "deepseek-v4-pro", sku}) {
			stock = []string{"deepseek-v4-flash", "deepseek-v4-pro", sku}
		} else if officialDeepSeekModelSetEquals(models, []string{"deepseek-v4-flash", "deepseek-v4-pro"}) && p.VisionModels == nil {
			stock = []string{"deepseek-v4-flash", "deepseek-v4-pro", sku}
		}
	}
	if stock == nil {
		return
	}
	if !p.HasModel(sku) {
		p.Models = mergeModelLists(models, []string{sku})
		p.Default = firstKnownModel(p.Default, p.Models, "deepseek-v4-flash")
	}
	if p.VisionModels == nil {
		p.VisionModels = OfficialDeepSeekPinnedVisionModels(p.ModelList())
	}
	backfillOfficialDeepSeekVisionPrice(p)
}

func backfillOfficialDeepSeekVisionPrice(p *ProviderEntry) {
	sku := openai.OfficialDeepSeekVisionModel
	if p == nil || !p.HasModel(sku) {
		return
	}
	if price, ok := pricingForModelKey(p.Prices, sku); ok && price != nil {
		return
	}
	currency := p.ProviderBillingCurrency()
	if currency == "" {
		currency = "USD"
	}
	price := deepSeekV4PriceForModel(currency, sku)
	if price == nil {
		return
	}
	if p.Prices == nil {
		p.Prices = map[string]*provider.Pricing{}
	}
	p.Prices[sku] = price
}

func officialDeepSeekModelSetEquals(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, model := range got {
		counts[strings.ToLower(strings.TrimSpace(model))]++
	}
	for _, model := range want {
		key := strings.ToLower(strings.TrimSpace(model))
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}
