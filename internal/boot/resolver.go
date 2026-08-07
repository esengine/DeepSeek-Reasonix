package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/extension"
	"reasonix/internal/extension/providerext"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

// LocalProviderResolver preserves the historical config-backed provider path.
type LocalProviderResolver struct {
	cfg   *config.Config
	proxy netclient.ProxySpec
}

func NewLocalProviderResolver(cfg *config.Config, proxy netclient.ProxySpec) *LocalProviderResolver {
	return &LocalProviderResolver{cfg: cfg, proxy: proxy}
}

func (r *LocalProviderResolver) Catalog() []provider.Descriptor {
	if r == nil || r.cfg == nil {
		return nil
	}
	out := make([]provider.Descriptor, 0, len(r.cfg.Providers))
	for i := range r.cfg.Providers {
		e := &r.cfg.Providers[i]
		ref := modelRefFromEntry(e)
		d := provider.Descriptor{
			Ref: ref, DisplayName: e.Name, Model: e.Model,
			ContextWindow: e.ContextWindow, Vision: config.EffectiveVision(e),
			Tools: true, DefaultEffort: config.EffectiveEffort(e),
		}
		if price := e.PriceForModel(e.Model); price != nil {
			d.PricingCurrency = price.Currency
			d.CacheHitPerMillion = price.CacheHit
			d.InputPerMillion = price.Input
			d.OutputPerMillion = price.Output
		}
		if len(e.SupportedEfforts) > 0 {
			d.Efforts = append([]string(nil), e.SupportedEfforts...)
			d.Reasoning = true
		}
		if config.ReasoningProtocolForEntry(e) == config.ReasoningProtocolDeepSeek {
			d.ToolCallReasoning = true
			d.Reasoning = true
		}
		out = append(out, d)
	}
	return out
}

func (r *LocalProviderResolver) Resolve(selection provider.Selection) (provider.Provider, error) {
	if r == nil || r.cfg == nil {
		return nil, fmt.Errorf("local provider resolver is not configured")
	}
	ref := strings.TrimSpace(selection.Ref)
	if ref == "" {
		return nil, fmt.Errorf("provider selection ref is required")
	}
	entry, ok := r.cfg.ResolveModel(ref)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownModel, ref)
	}
	if selection.Effort != nil {
		entry.Effort = *selection.Effort
	}
	return NewProviderWithProxy(entry, r.proxy)
}

func resolveProvider(resolver provider.Resolver, cfg *config.Config, proxy netclient.ProxySpec, selection provider.Selection) (provider.Provider, error) {
	if resolver != nil {
		return resolver.Resolve(selection)
	}
	return NewLocalProviderResolver(cfg, proxy).Resolve(selection)
}

// mergeSidecarProviders wraps the build's resolver with the extension-hosted
// provider adapter (stage 7) whenever a started sidecar declared providers in
// its handshake. The base resolver — the caller-owned broker when
// opts.ProviderResolver is set, the local config-backed one otherwise — keeps
// serving every non-plugin ref; plugin providers are additive on top of it,
// and an exact-ref collision without the plugin's provider:<ref> claim is a
// *providerext.ConflictError (fatal at the call site). The merged Resolver is
// also the sidecar clients' stream router, installed here so inbound
// stream/chunk and stream/end notifications reach the buffered streams.
func mergeSidecarProviders(base provider.Resolver, mgr *sidecar.Manager, claims map[extension.Slot]extension.ContributionSource) (provider.Resolver, error) {
	if mgr == nil {
		return base, nil
	}
	declares := false
	for _, client := range mgr.Clients() {
		if len(client.Handshake().Providers) > 0 {
			declares = true
			break
		}
	}
	if !declares {
		return base, nil
	}
	clientsFn := func() []providerext.ProviderClient {
		clients := mgr.Clients()
		out := make([]providerext.ProviderClient, 0, len(clients))
		for _, client := range clients {
			out = append(out, client)
		}
		return out
	}
	merged, err := providerext.New(base, clientsFn, claims)
	if err != nil {
		return nil, err
	}
	for _, client := range mgr.Clients() {
		client.SetStreamRouter(merged)
	}
	return merged, nil
}

func modelRefFromEntry(e *config.ProviderEntry) string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Model) == "" {
		return e.Name
	}
	return e.Name + "/" + e.Model
}

// resolveModelEntry synthesizes only non-secret metadata from the resolver
// catalog. A caller-owned (or extension-merged) resolver is authoritative even
// when the credential-free Host happens to contain a provider with the same
// ref. The unknown-model error names every ref the session could have used,
// including plugin-namespaced refs a merged extension resolver serves.
func resolveModelEntry(resolver provider.Resolver, cfg *config.Config, modelName string) (*config.ProviderEntry, string, error) {
	if resolver != nil {
		entry := syntheticEntryFromResolver(resolver, modelName)
		if strings.TrimSpace(entry.Name) != "" {
			return entry, modelRefFromEntry(entry), nil
		}
	}
	if entry, ok := cfg.ResolveModel(modelName); ok {
		return entry, modelRefFromEntry(entry), nil
	}
	available := providerNames(cfg)
	if pluginRefs := extensionCatalogRefs(resolver); len(pluginRefs) > 0 {
		if available != "" {
			available += "/"
		}
		available += strings.Join(pluginRefs, "/")
	}
	return nil, "", fmt.Errorf("%w %q (configured: %s); note: defining [[providers]] replaces the built-in presets, so add a [[providers]] entry for it or use a configured name, or run `reasonix setup` to reconfigure", ErrUnknownModel, modelName, available)
}

// extensionCatalogRefs returns the plugin-namespaced refs a resolver's catalog
// serves, for error messages and pickers that merge extension providers with
// the config's own. Nil-safe: no resolver (or no plugin refs) → nil.
func extensionCatalogRefs(resolver provider.Resolver) []string {
	if resolver == nil {
		return nil
	}
	var out []string
	for _, d := range resolver.Catalog() {
		if providerext.PluginRefOwner(d.Ref) != "" {
			out = append(out, d.Ref)
		}
	}
	return out
}

func resolveOptionalEntry(resolver provider.Resolver, cfg *config.Config, ref string) (*config.ProviderEntry, bool) {
	if resolver != nil {
		entry := syntheticEntryFromResolver(resolver, ref)
		if strings.TrimSpace(entry.Name) != "" {
			return entry, true
		}
	}
	entry, ok := cfg.ResolveModel(ref)
	return entry, ok
}

func syntheticEntryFromResolver(r provider.Resolver, ref string) *config.ProviderEntry {
	ref = strings.TrimSpace(ref)
	if r == nil || ref == "" {
		return &config.ProviderEntry{}
	}
	var match *provider.Descriptor
	for _, d := range r.Catalog() {
		if d.Ref == ref || d.DisplayName == ref || d.Model == ref || strings.HasPrefix(d.Ref, ref+"/") {
			copy := d
			match = &copy
			break
		}
	}
	if match == nil {
		return &config.ProviderEntry{}
	}
	name, model := splitProviderRef(match.Ref)
	if model == "" {
		model = match.Model
	}
	if name == "" {
		name = match.DisplayName
	}
	contextWindow := match.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 128_000
	}
	entry := &config.ProviderEntry{
		Name: name, Model: model, ContextWindow: contextWindow,
		SupportedEfforts: append([]string(nil), match.Efforts...),
		DefaultEffort:    match.DefaultEffort, Vision: match.Vision,
	}
	if match.CacheHitPerMillion > 0 || match.InputPerMillion > 0 || match.OutputPerMillion > 0 {
		entry.Price = &provider.Pricing{CacheHit: match.CacheHitPerMillion, Input: match.InputPerMillion, Output: match.OutputPerMillion, Currency: match.PricingCurrency}
	}
	return entry
}

func splitProviderRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if before, after, ok := strings.Cut(ref, "/"); ok {
		return before, after
	}
	return ref, ""
}
