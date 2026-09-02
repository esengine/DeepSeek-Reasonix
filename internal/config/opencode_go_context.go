package config

import (
	"strconv"
	"strings"

	"reasonix/internal/billing"
	"reasonix/internal/provider"
)

const (
	legacyOpenCodeGoChatWindow      = 128000
	legacyOpenCodeGoAnthropicWindow = 262144
)

func normalizeLegacyOpenCodeGoInstalls(c *Config) bool {
	changed := normalizeLegacyOpenCodeGoKimiK3Catalog(c)
	changed = normalizeLegacyOpenCodeGoVisionCatalog(c) || changed
	changed = normalizeLegacyOpenCodeGoRouteCatalog(c) || changed
	changed = normalizeLegacyOpenCodeGoContextWindows(c) || changed
	changed = normalizeLegacyOpenCodeGoBilling(c) || changed
	return changed
}

// normalizeLegacyOpenCodeGoRouteCatalog repairs the old aggregate model list
// shape. OpenCode Go exposes one /models catalog, but the wire protocol is
// route-specific: Chat, Anthropic Messages, and Responses do not accept the
// same model IDs. Older installs could therefore persist Grok/Qwen under the
// wrong provider kind and only discover the mistake after a paid request.
//
// The migration is additive and non-destructive. The original provider name is
// kept for the route it already serves; known models belonging to another
// route are moved to a sibling provider with the same key, headers, prices,
// overrides, and user-owned fields. Unknown/future model IDs stay on the
// original route so forward compatibility is preserved.
func normalizeLegacyOpenCodeGoRouteCatalog(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := 0; i < len(c.Providers); i++ {
		p := &c.Providers[i]
		if strings.TrimSpace(p.RequestURL) != "" {
			// An explicit request_url is a user-owned escape hatch; the base URL
			// alone cannot prove which wire route that custom URL implements.
			continue
		}
		currentRoute, ok := provider.OfficialOpenCodeGoRoute(p.Kind, p.BaseURL)
		if !ok || len(p.ModelList()) == 0 {
			continue
		}
		models := append([]string(nil), p.ModelList()...)
		groups := map[string][]string{}
		for _, model := range models {
			routes := openCodeGoModelRoutes(model)
			route := currentRoute
			if !providerModelSupportsOpenCodeGoRoute(currentRoute, p.BaseURL, p.Kind, model) && len(routes) == 1 {
				route = routes[0]
			}
			groups[route] = append(groups[route], model)
		}
		if len(groups) == 1 {
			if target := firstOpenCodeGoGroup(groups); target != currentRoute {
				setOpenCodeGoRoute(p, target)
				p.PresetID = openCodeGoPresetIDForRoute(target)
				p.PresetVersion = ProviderPresetVersion
				changed = true
			}
			continue
		}
		currentModels := groups[currentRoute]
		if len(currentModels) == 0 {
			// Preserve the old provider identity on the route selected by its
			// default model; other groups become siblings below.
			preferred := strings.TrimSpace(p.DefaultModel())
			if routes := openCodeGoModelRoutes(preferred); len(routes) == 1 {
				currentRoute = routes[0]
				setOpenCodeGoRoute(p, currentRoute)
				currentModels = groups[currentRoute]
				changed = true
			}
		}
		if len(currentModels) == 0 {
			continue
		}
		original := cloneProviderEntry(*p)
		originalDefault := original.DefaultModel()
		if !stringSlicesEqual(p.ModelList(), currentModels) || len(p.Models) == 0 && len(p.Model) > 0 {
			applyOpenCodeGoModelGroup(p, currentModels)
			changed = true
		}
		if mergeOpenCodeGoRouteDefaults(p, currentRoute) {
			changed = true
		}
		for route, routeModels := range groups {
			if route == currentRoute || len(routeModels) == 0 {
				continue
			}
			sibling := cloneProviderEntry(original)
			sibling.Name = uniqueOpenCodeGoSiblingName(c, p.Name, route)
			sibling.PresetID = openCodeGoPresetIDForRoute(route)
			sibling.PresetVersion = ProviderPresetVersion
			setOpenCodeGoRoute(&sibling, route)
			if sibling.PresetID == "opencode-go" && sibling.ContextWindow == legacyOpenCodeGoChatWindow {
				sibling.ContextWindow = openCodeGoCanonicalContextWindow(route)
			}
			applyOpenCodeGoModelGroup(&sibling, routeModels)
			mergeOpenCodeGoRouteDefaults(&sibling, route)
			retargetOpenCodeGoRefs(c, p.Name, routeModels, sibling.Name, originalDefault)
			addOpenCodeGoAccess(c, p.Name, sibling.Name)
			c.Providers = append(c.Providers, sibling)
			changed = true
		}
	}
	return changed
}

func openCodeGoModelRoutes(model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	routes := make([]string, 0, 3)
	if _, ok := provider.LookupOfficialOpenCodeGo("openai", "https://opencode.ai/zen/go/v1", model); ok {
		routes = append(routes, provider.OpenCodeGoRouteChat)
	}
	if _, ok := provider.LookupOfficialOpenCodeGo("anthropic", "https://opencode.ai/zen/go", model); ok {
		routes = append(routes, provider.OpenCodeGoRouteAnthropic)
	}
	if _, ok := provider.LookupOfficialOpenCodeGo("responses", "https://opencode.ai/zen/go/v1", model); ok {
		routes = append(routes, provider.OpenCodeGoRouteResponses)
	}
	return routes
}

func providerModelSupportsOpenCodeGoRoute(route, baseURL, kind, model string) bool {
	_, ok := provider.LookupOfficialOpenCodeGo(kind, baseURL, model)
	if ok {
		return true
	}
	// A DeepSeek model is intentionally supported by all three OpenCode Go
	// adapters; the lookup above already captures that special compatibility
	// path when the current route is official.
	return route == provider.OpenCodeGoRouteChat && strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}

func firstOpenCodeGoGroup(groups map[string][]string) string {
	for _, route := range []string{provider.OpenCodeGoRouteChat, provider.OpenCodeGoRouteAnthropic, provider.OpenCodeGoRouteResponses} {
		if len(groups[route]) > 0 {
			return route
		}
	}
	return ""
}

func setOpenCodeGoRoute(p *ProviderEntry, route string) {
	if p == nil {
		return
	}
	switch route {
	case provider.OpenCodeGoRouteChat:
		p.Kind = "openai"
		p.BaseURL = "https://opencode.ai/zen/go/v1"
		p.ResponsesMode = ""
	case provider.OpenCodeGoRouteAnthropic:
		p.Kind = "anthropic"
		p.BaseURL = "https://opencode.ai/zen/go"
		p.ResponsesMode = ""
	case provider.OpenCodeGoRouteResponses:
		p.Kind = "responses"
		p.BaseURL = "https://opencode.ai/zen/go/v1"
		p.ResponsesMode = "stateless"
	}
}

func openCodeGoPresetIDForRoute(route string) string {
	switch route {
	case provider.OpenCodeGoRouteAnthropic:
		return "opencode-go-anthropic"
	case provider.OpenCodeGoRouteResponses:
		return "opencode-go-responses"
	default:
		return "opencode-go"
	}
}

func openCodeGoCanonicalContextWindow(route string) int {
	switch route {
	case provider.OpenCodeGoRouteAnthropic:
		return legacyOpenCodeGoAnthropicWindow
	case provider.OpenCodeGoRouteResponses:
		return 500_000
	default:
		return 128_000
	}
}

func mergeOpenCodeGoRouteDefaults(p *ProviderEntry, route string) bool {
	if p == nil {
		return false
	}
	preset, ok := CuratedProviderPreset(openCodeGoPresetIDForRoute(route))
	if !ok || len(preset.Entries) != 1 {
		return false
	}
	return mergeMissingOpenCodeGoContextOverrides(p, preset.Entries[0].ModelOverrides)
}

func applyOpenCodeGoModelGroup(p *ProviderEntry, models []string) {
	if p == nil || len(models) == 0 {
		return
	}
	originalDefault := strings.TrimSpace(p.DefaultModel())
	if len(p.Models) > 0 || len(models) > 1 {
		p.Models = append([]string(nil), models...)
		p.Model = ""
	} else {
		p.Models = nil
		p.Model = models[0]
	}
	if containsModelFold(models, originalDefault) {
		p.Default = originalDefault
	} else {
		p.Default = models[0]
	}
	filterOpenCodeGoModelFields(p, models)
}

func filterOpenCodeGoModelFields(p *ProviderEntry, models []string) {
	if p == nil {
		return
	}
	wanted := map[string]bool{}
	for _, model := range models {
		wanted[strings.ToLower(strings.TrimSpace(model))] = true
	}
	filterStrings := func(values []string) []string {
		if values == nil {
			return nil
		}
		out := make([]string, 0, len(values))
		for _, value := range values {
			if wanted[strings.ToLower(strings.TrimSpace(value))] {
				out = append(out, value)
			}
		}
		return out
	}
	p.VisionModels = filterStrings(p.VisionModels)
	for model := range p.ModelOverrides {
		if !wanted[strings.ToLower(strings.TrimSpace(model))] {
			delete(p.ModelOverrides, model)
		}
	}
	for model := range p.Prices {
		if !wanted[strings.ToLower(strings.TrimSpace(model))] {
			delete(p.Prices, model)
		}
	}
}

func containsModelFold(models []string, want string) bool {
	for _, model := range models {
		if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func uniqueOpenCodeGoSiblingName(c *Config, base, route string) string {
	name := strings.TrimSpace(base) + "-" + route
	if _, ok := c.Provider(name); !ok {
		return name
	}
	for n := 2; ; n++ {
		candidate := name + "-" + strconv.Itoa(n)
		if _, ok := c.Provider(candidate); !ok {
			return candidate
		}
	}
}

func addOpenCodeGoAccess(c *Config, names ...string) {
	if c == nil || c.Desktop.ProviderAccess == nil {
		return
	}
	seen := desktopProviderAccessMap(c.Desktop.ProviderAccess)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, name)
			seen[name] = true
		}
	}
}

func retargetOpenCodeGoRefs(c *Config, oldName string, models []string, newName, oldDefault string) {
	if c == nil {
		return
	}
	wanted := map[string]bool{}
	for _, model := range models {
		wanted[strings.ToLower(strings.TrimSpace(model))] = true
	}
	rewrite := func(ref string) string {
		ref = strings.TrimSpace(ref)
		if model, ok := strings.CutPrefix(ref, oldName+"/"); ok {
			if wanted[strings.ToLower(strings.TrimSpace(model))] {
				return newName + "/" + model
			}
		}
		if strings.EqualFold(ref, oldName) && wanted[strings.ToLower(strings.TrimSpace(oldDefault))] {
			return newName + "/" + oldDefault
		}
		if wanted[strings.ToLower(ref)] {
			return newName + "/" + ref
		}
		return ref
	}
	c.DefaultModel = rewrite(c.DefaultModel)
	c.Agent.PlannerModel = rewrite(c.Agent.PlannerModel)
	c.Agent.VisionModel = rewrite(c.Agent.VisionModel)
	c.Agent.GuardianModel = rewrite(c.Agent.GuardianModel)
	c.Agent.RecoveryModel = rewrite(c.Agent.RecoveryModel)
	c.Agent.SubagentModel = rewrite(c.Agent.SubagentModel)
	for name, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[name] = rewrite(ref)
	}
	c.Bot.Model = rewrite(c.Bot.Model)
	for i := range c.Bot.Connections {
		c.Bot.Connections[i].Model = rewrite(c.Bot.Connections[i].Model)
	}
}

func normalizeLegacyOpenCodeGoBilling(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		if _, ok := provider.OfficialOpenCodeGoRoute(p.Kind, p.BaseURL); !ok || strings.TrimSpace(p.BillingMode) != "" {
			continue
		}
		p.BillingMode = billing.BillingModeSubscriptionEquivalent
		changed = true
	}
	return changed
}

func officialOpenCodeGoKindURL(kind, baseURL string) bool {
	_, ok := provider.OfficialOpenCodeGoRoute(kind, baseURL)
	return ok
}

func openCodeGoPresetIDForMigration(p ProviderEntry) string {
	presetID := strings.TrimSpace(p.PresetID)
	if presetID == "" {
		presetID = strings.TrimSpace(p.Name)
	}
	switch presetID {
	case "opencode-go", "opencode-go-anthropic", "opencode-go-deepseek-anthropic", "opencode-go-deepseek-responses":
		return presetID
	default:
		return ""
	}
}

func mergeMissingOpenCodeGoContextOverrides(p *ProviderEntry, defaults map[string]ProviderModelOverride) bool {
	if p == nil || len(defaults) == 0 {
		return false
	}
	if p.ModelOverrides == nil {
		p.ModelOverrides = map[string]ProviderModelOverride{}
	}
	changed := false
	for defaultKey, defaultOverride := range defaults {
		overrideKey := defaultKey
		for key := range p.ModelOverrides {
			if strings.EqualFold(strings.TrimSpace(key), defaultKey) {
				overrideKey = key
				break
			}
		}
		override := p.ModelOverrides[overrideKey]
		if override.ContextWindow == 0 && defaultOverride.ContextWindow > 0 {
			override.ContextWindow = defaultOverride.ContextWindow
			p.ModelOverrides[overrideKey] = override
			changed = true
		}
		if override.MaxOutputTokens == 0 && defaultOverride.MaxOutputTokens != 0 {
			override.MaxOutputTokens = defaultOverride.MaxOutputTokens
			p.ModelOverrides[overrideKey] = override
			changed = true
		}
	}
	return changed
}

func openCodeGoCatalogMatches(presetID string, p, canonical ProviderEntry) bool {
	if strings.TrimSpace(p.Model) != "" {
		return false
	}
	if stringSlicesEqual(p.Models, canonical.Models) {
		return true
	}
	return presetID == "opencode-go" && (stringSlicesEqual(p.Models, opencodeGoModels) || stringSlicesEqual(p.Models, legacyOpenCodeGoModels))
}

func migrateOpenCodeGoProviderWindow(p *ProviderEntry, canonical ProviderEntry, legacy int) bool {
	if p.ContextWindow != 0 && p.ContextWindow != legacy {
		return false
	}
	if p.ContextWindow == canonical.ContextWindow {
		return mergeMissingOpenCodeGoContextOverrides(p, canonical.ModelOverrides)
	}
	p.ContextWindow = canonical.ContextWindow
	_ = mergeMissingOpenCodeGoContextOverrides(p, canonical.ModelOverrides)
	return true
}

// normalizeLegacyOpenCodeGoContextWindows upgrades only unmodified official
// OpenCode Go presets that still carry the old conservative provider window.
// Custom catalogs, endpoints, and positive context/output overrides stay put.
func normalizeLegacyOpenCodeGoContextWindows(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		presetID := openCodeGoPresetIDForMigration(*p)
		preset, ok := CuratedProviderPreset(presetID)
		if presetID == "" || !ok || len(preset.Entries) != 1 {
			continue
		}
		canonical := preset.Entries[0]
		if !strings.EqualFold(strings.TrimSpace(p.Kind), strings.TrimSpace(canonical.Kind)) ||
			!officialOpenCodeGoKindURL(canonical.Kind, p.BaseURL) ||
			normalizedBaseURLForMigration(p.BaseURL) != normalizedBaseURLForMigration(canonical.BaseURL) ||
			!openCodeGoCatalogMatches(presetID, *p, canonical) {
			continue
		}
		legacy := legacyOpenCodeGoChatWindow
		if presetID == "opencode-go-anthropic" || presetID == "opencode-go-deepseek-anthropic" {
			legacy = legacyOpenCodeGoAnthropicWindow
		}
		if migrateOpenCodeGoProviderWindow(p, canonical, legacy) {
			changed = true
		}
	}
	return changed
}
