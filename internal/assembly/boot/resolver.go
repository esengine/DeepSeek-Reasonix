package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/providerext"
	"reasonix/internal/ext/extension/sidecar"
)

// LocalProviderResolver preserves the historical config-backed provider path.
type LocalProviderResolver struct {
	cfg   *config.Config
	proxy netclient.ProxySpec
}

func NewLocalProviderResolver(cfg *config.Config, proxy netclient.ProxySpec) *LocalProviderResolver {
	return &LocalProviderResolver{cfg: cfg, proxy: proxy}
}

// Catalog lists every chat model the config declares, one ref per model, and
// marks the one a new session starts on. A broker's remote reads nothing else
// about this machine's models, so a provider listed once by name would leave
// it unable to recognise any ref but that provider's first.
func (r *LocalProviderResolver) Catalog() []provider.Descriptor {
	if r == nil || r.cfg == nil {
		return nil
	}
	defaultRef := ""
	if def, ok := startupChatModel(r.cfg); ok {
		if e, found := r.cfg.ResolveModel(def.Ref); found {
			defaultRef = modelRefFromEntry(e)
		}
	}
	var out []provider.Descriptor
	seen := map[string]bool{}
	for i := range r.cfg.Providers {
		p := &r.cfg.Providers[i]
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		for _, model := range models {
			e, ok := r.cfg.ResolveModel(p.Name + "/" + model)
			if !ok {
				continue
			}
			ref := modelRefFromEntry(e)
			if seen[ref] {
				continue
			}
			seen[ref] = true
			d := descriptorFor(e, ref)
			d.Default = ref == defaultRef
			out = append(out, d)
		}
	}
	return out
}

func descriptorFor(e *config.ProviderEntry, ref string) provider.Descriptor {
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
	// The catalog must expose the same model-specific ladder as /effort.
	capability := config.EffortCapabilityForEntry(e)
	switch {
	case len(e.SupportedEfforts) > 0:
		d.Efforts = append([]string(nil), e.SupportedEfforts...)
		d.Reasoning = true
	case capability.Supported:
		d.Efforts = capability.Levels
		d.Reasoning = true
	}
	if d.DefaultEffort == "" {
		d.DefaultEffort = capability.Default
	}
	d.ForcesThinking = config.EffortForcesThinking(e)
	if config.ReasoningProtocolForEntry(e) == config.ReasoningProtocolDeepSeek {
		d.ToolCallReasoning = true
		d.Reasoning = true
	}
	return d
}

// newSessionModel names the model a build starts on when its caller named none.
// A caller-owned resolver answers first: its models are the only ones this
// session reaches, and a default read from this machine's config can name one
// it cannot run. A resolver that marks none leaves the config to answer.
func newSessionModel(resolver provider.Resolver, cfg *config.Config, fallback bool) (ref, skippedDefault string) {
	if resolver != nil {
		if ref := provider.DefaultRef(resolver.Catalog()); ref != "" {
			return ref, ""
		}
	}
	resolve := cfg.ResolveNewSessionChatModel
	if fallback {
		resolve = func() (config.NewSessionModel, bool) { return startupChatModel(cfg) }
	}
	if m, ok := resolve(); ok {
		return m.Ref, m.SkippedDefault
	}
	return "", ""
}

// startupChatModel is what a build nobody named a model for opens on. A
// plugin-owned default is the extension resolver's to answer, so it is kept.
func startupChatModel(cfg *config.Config) (config.NewSessionModel, bool) {
	if cfg == nil {
		return config.NewSessionModel{}, false
	}
	if providerext.PluginRefOwner(strings.TrimSpace(cfg.DefaultModel)) != "" {
		m, ok := cfg.ResolveNewSessionChatModel()
		m.SkippedDefault = ""
		return m, ok
	}
	return cfg.ResolveStartupChatModel()
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

// LiveProviderResolver reads the configuration on each call instead of closing
// over one load. What it serves outlives the edits made while it runs: a
// provider added in Settings has to reach whoever asks next, without restarting
// the process — or the SSH link — that has been holding the old answer.
type LiveProviderResolver struct{}

func (LiveProviderResolver) current() *LocalProviderResolver {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	return NewLocalProviderResolver(cfg, cfg.NetworkProxySpec())
}

// Catalog offers what this machine can answer with now. A provider without a
// stored key is still declared, but every request the far side sent it would
// fail here, so it stays out of the list that side picks from.
func (r LiveProviderResolver) Catalog() []provider.Descriptor {
	cur := r.current()
	if cur == nil {
		return nil
	}
	var out []provider.Descriptor
	for _, d := range cur.Catalog() {
		if e, ok := cur.cfg.ResolveModel(d.Ref); ok && e.Configured() {
			out = append(out, d)
		}
	}
	return out
}

func (r LiveProviderResolver) Resolve(selection provider.Selection) (provider.Provider, error) {
	cur := r.current()
	if cur == nil {
		return nil, fmt.Errorf("the configuration could not be read")
	}
	return cur.Resolve(selection)
}

func resolveProvider(resolver provider.Resolver, cfg *config.Config, proxy netclient.ProxySpec, selection provider.Selection) (provider.Provider, error) {
	if resolver != nil {
		return resolver.Resolve(selection)
	}
	return NewLocalProviderResolver(cfg, proxy).Resolve(selection)
}

// mergeSidecarProviders wraps the build's resolver with the extension-hosted
// provider adapter whenever a started sidecar declared providers.
// It does not install stream routers — call installSidecarStreamRouters after
// commit so failed narrow rebuilds never leave adopted clients on a discarded
// generation resolver.
func mergeSidecarProviders(base provider.Resolver, mgr *sidecar.Manager, claims map[extension.Slot]extension.ContributionSource, owners ...*extension.RuntimeOwner) (provider.Resolver, error) {
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
	return providerext.New(base, clientsFn, claims, owners...)
}

// installSidecarStreamRouters binds merged as the stream router on every live
// client. Call only after cold-start success or narrow-rebuild commit.
func installSidecarStreamRouters(mgr *sidecar.Manager, merged provider.Resolver) {
	if mgr == nil || merged == nil {
		return
	}
	router, ok := merged.(sidecar.StreamRouter)
	if !ok {
		return
	}
	for _, client := range mgr.Clients() {
		client.SetStreamRouter(router)
	}
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
