// modelcaps.go — what a picker may truthfully say about one model.
package serve

import (
	"net/url"
	"strings"

	"reasonix/internal/contract/config"
)

// modelPrice is list price per 1M tokens, as the provider entry declares it.
type modelPrice struct {
	Input    float64 `json:"input"`
	Output   float64 `json:"output"`
	CacheHit float64 `json:"cacheHit,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

// describeModel fills the capability half of an entry from a resolved provider.
// Every field stays zero unless something declares it: an inferred "reads
// images" badge sends the user to a rejected request they cannot explain, so
// absence is reported as absence rather than guessed at.
func describeModel(e *config.ProviderEntry, into *modelEntry) {
	if e == nil || into == nil {
		return
	}
	into.Vendor = vendorOf(e.BaseURL)
	into.KeyEnv = e.APIKeyEnv
	into.DisplayName = strings.TrimSpace(e.DisplayName)
	into.Preset = strings.TrimSpace(e.PresetID) != ""
	into.Vision = config.EffectiveVision(e)
	if capability := config.EffortCapabilityForEntry(e); capability.Supported {
		into.Efforts = capability.Levels
		into.Effort = capability.Default
		into.ForcesThinking = config.EffortForcesThinking(e)
	}
	into.ContextWindow = e.ContextWindow
	if p := e.Price; p != nil && (p.Input > 0 || p.Output > 0) {
		into.Price = &modelPrice{Input: p.Input, Output: p.Output, CacheHit: p.CacheHit, Currency: p.Currency}
	}
}

// vendorOf names the endpoint two config entries share. One host reached under
// two protocols is one service with two routes, and a picker listing them as
// separate providers shows the same model twice with nothing to choose between.
func vendorOf(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// capabilitiesFor resolves one configured model back through the config so the
// per-model price and override layers apply. It answers nothing when resolution
// lands on a different provider — the legacy desktop aliases retarget refs, and
// another entry's capabilities would be worse than none.
func capabilitiesFor(cfg *config.Config, p *config.ProviderEntry, model string, into *modelEntry) {
	if cfg == nil || p == nil {
		return
	}
	resolved, ok := cfg.ResolveModel(p.Name + "/" + model)
	if !ok || resolved.Name != p.Name || resolved.Model != model {
		into.Vendor = vendorOf(p.BaseURL)
		into.KeyEnv = p.APIKeyEnv
		into.DisplayName = strings.TrimSpace(p.DisplayName)
		into.Preset = strings.TrimSpace(p.PresetID) != ""
		return
	}
	describeModel(resolved, into)
}
