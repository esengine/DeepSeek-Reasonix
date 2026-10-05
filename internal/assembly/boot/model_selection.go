package boot

import (
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension/providerext"
	"reasonix/internal/runtime/capability"
)

// modelSelection is the executor model this build runs and the preset it
// runs under.
type modelSelection struct {
	name     string
	skipped  string // a default_model nothing configured serves, passed over
	ref      string
	entry    *config.ProviderEntry
	preset   string
	delivery bool
	profile  capability.Profile
}

// selectModel resolves the executor model. The caller-owned broker is
// authoritative for every ref; the extension resolver owns only plugin refs, so
// a config ref keeps its full entry — endpoint, credentials, balance URL.
func selectModel(opts Options, cfg *config.Config, extensionResolver provider.Resolver) (modelSelection, error) {
	var m modelSelection
	// A keyless default_model falls through to the next configured chat model
	// (#6996); an explicit opts.Model still fails loudly.
	m.name = opts.Model
	if m.name == "" {
		m.name, m.skipped = newSessionModel(opts.ProviderResolver, cfg, opts.OpenOnFallbackModel)
	}
	config.NormalizeLegacyMimoCustomProvidersForRefs(cfg, m.name)
	m.preset = strings.TrimSpace(opts.AgentPreset)
	if m.preset == "" {
		m.preset = AgentPresetFromTokenMode(opts.TokenMode)
	}
	m.preset = NormalizeAgentPreset(m.preset)
	m.delivery = m.preset == AgentPresetDelivery
	m.profile = capability.ProfileBalanced
	if m.delivery {
		m.profile = capability.ProfileDelivery
	}
	entryResolver := opts.ProviderResolver
	pluginRef := providerext.PluginRefOwner(m.name) != ""
	if entryResolver == nil && extensionResolver != nil && pluginRef {
		entryResolver = extensionResolver
	}
	var err error
	m.entry, m.ref, err = resolveModelEntry(entryResolver, cfg, m.name)
	if err != nil {
		return m, err
	}
	if opts.EffortOverride != nil {
		m.entry.Effort = *opts.EffortOverride
		if m.entry.Kind == "anthropic" && strings.TrimSpace(m.entry.Effort) != "" && strings.TrimSpace(m.entry.Thinking) == "" {
			m.entry.Thinking = "adaptive"
		}
	}
	// A plugin ref carries no config credential; the extension provider holds its own keys.
	if opts.RequireKey && opts.ProviderResolver == nil && !pluginRef {
		if err := cfg.Validate(m.name); err != nil {
			return m, err
		}
	}
	return m, nil
}
