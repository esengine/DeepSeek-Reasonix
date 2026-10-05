package config

import (
	"reasonix/internal/contract/pricing"
	"strings"
)

// The vendor folded its Flash names into one model that reads images, so a
// stored config keeps offering a name whose model is gone, beside a vision
// connection that has become a duplicate of the flash one.

// DeepSeekFlashModel is the model behind every Flash name the endpoint accepts.
// It reads images, which is why nothing here ships a second entry for that: what
// a fresh install gets, what a retired name migrates to, and the model an image
// notice may point at are all this one string.
const DeepSeekFlashModel = "deepseek-flash"

// deepSeekProModel kept its own name, rates and text-only reach past that fold.
const deepSeekProModel = "deepseek-v4-pro"

// retiredDeepSeekFlashModels still answer, and answer as DeepSeekFlashModel at
// its price — measured 2026-09-13, including the image read the first of them
// never had. Migrated rather than kept: the reply echoes the name it was asked
// by, so a list carrying one of these reports a model nobody serves.
var retiredDeepSeekFlashModels = pricing.RetiredDeepSeekFlashModels()

// retiredDeepSeekVisionProvider was the connection shipped when reading images
// needed a model of its own. It folds into the flash entry, which now does that.
const retiredDeepSeekVisionProvider = "deepseek-vision"

// deepSeekDefaultProviders is what a fresh install ships. Anthropic-compatible
// Messages, so provider-executed web search is on by default; existing explicit
// entries merge on top and keep the protocol they were configured with.
func deepSeekDefaultProviders() []ProviderEntry {
	return []ProviderEntry{
		{
			Name: "deepseek-flash", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
			Model: DeepSeekFlashModel, APIKeyEnv: "DEEPSEEK_API_KEY",
			VisionModels: []string{DeepSeekFlashModel},
			BalanceURL:   "https://api.deepseek.com/user/balance", Thinking: "enabled",
			WebSearch: new(true), SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "low",
			ContextWindow: 1_000_000, Price: deepSeekOfficialRate(DeepSeekFlashModel, "USD"),
			BillingCurrency: "USD", BillingMode: "payg",
		},
		{
			Name: "deepseek-pro", Kind: "anthropic", BaseURL: deepSeekAnthropicBaseURL,
			Model: deepSeekProModel, APIKeyEnv: "DEEPSEEK_API_KEY",
			BalanceURL: "https://api.deepseek.com/user/balance", Thinking: "enabled",
			WebSearch: new(true), SupportedEfforts: []string{"disabled", "high", "max"}, DefaultEffort: "high",
			ContextWindow: 1_000_000, Price: deepSeekOfficialRate(deepSeekProModel, "USD"),
			BillingCurrency: "USD", BillingMode: "payg",
		},
	}
}

// normalizeOfficialDeepSeekModels brings a loaded config up to the catalog the
// vendor serves today. It reports whether it changed anything, so a load for
// edit persists that and a plain load only holds it in memory.
func normalizeOfficialDeepSeekModels(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderHost(p.BaseURL) != "api.deepseek.com" {
			continue
		}
		changed = migrateRetiredDeepSeekFlashModels(p) || changed
		switch strings.TrimSpace(p.Name) {
		case "deepseek":
			required := []string{DeepSeekFlashModel, deepSeekProModel}
			if strings.EqualFold(strings.TrimSpace(p.Kind), "responses") {
				required = required[:1]
			}
			ensureProviderModels(p, required, DeepSeekFlashModel)
		case "deepseek-flash", retiredDeepSeekVisionProvider:
			ensureProviderModels(p, []string{DeepSeekFlashModel}, DeepSeekFlashModel)
		case "deepseek-pro":
			ensureProviderModels(p, []string{deepSeekProModel}, deepSeekProModel)
		}
		changed = tickDeepSeekFlashVision(p) || changed
		backfillDeepSeekAnthropicCapabilities(p)
	}
	// Each call must run: ORing them the other way round short-circuits the
	// fold as soon as an entry above has already reported a change.
	changed = migrateRetiredDeepSeekModelRefs(c) || changed
	changed = foldRetiredDeepSeekVisionProvider(c) || changed
	return changed
}

// migrateRetiredDeepSeekFlashModels points one official entry at the model the
// endpoint serves for it. Every per-model map is re-keyed too: a price or an
// effort list left under a retired name stops answering for the model that
// replaced it, which is how an edited rate silently reverts to the default.
func migrateRetiredDeepSeekFlashModels(p *ProviderEntry) bool {
	if p == nil || officialProviderHost(p.BaseURL) != "api.deepseek.com" {
		return false
	}
	changed := false
	for _, retired := range retiredDeepSeekFlashModels {
		changed = renameProviderModel(p, retired, DeepSeekFlashModel) || changed
	}
	return changed
}

// tickDeepSeekFlashVision marks the flash model as image-taking unless the user
// has said something about vision on this entry. Listed but unticked is the
// worst of both: pro takes an image and answers as if it saw nothing rather
// than refusing it, so an unticked flash would drop images just as quietly.
func tickDeepSeekFlashVision(p *ProviderEntry) bool {
	if p == nil || officialProviderHost(p.BaseURL) != "api.deepseek.com" ||
		len(p.VisionModels) > 0 || !p.HasModel(DeepSeekFlashModel) {
		return false
	}
	p.VisionModels = []string{DeepSeekFlashModel}
	return true
}
