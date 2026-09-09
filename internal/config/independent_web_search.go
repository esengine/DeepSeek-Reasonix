package config

import (
	"fmt"
	"slices"
	"strings"
)

// IsOfficialDeepSeekSearchEndpoint also recognizes Chat Completions accounts:
// their search requests use a separate official Messages endpoint. Explicit
// request URL overrides are never redirected to a different endpoint.
func IsOfficialDeepSeekSearchEndpoint(e *ProviderEntry) bool {
	if e == nil || e.RequestURL != "" || e.ChatURL != "" {
		return false
	}
	if IsOfficialDeepSeekWebSearchEndpoint(e) {
		return true
	}
	if !strings.EqualFold(e.Kind, "openai") {
		return false
	}
	copy := *e
	copy.Kind = "responses"
	copy.BaseURL = strings.TrimSuffix(strings.TrimRight(e.BaseURL, "/"), "/v1")
	return IsOfficialDeepSeekWebSearchEndpoint(&copy)
}

// EffectiveIndependentWebSearch preserves the existing tri-state switch while
// allowing official Chat Completions accounts to supply an auxiliary search.
func EffectiveIndependentWebSearch(e *ProviderEntry) bool {
	if e == nil || (!SupportsServerWebSearch(e) && !IsOfficialDeepSeekSearchEndpoint(e)) {
		return false
	}
	if e.WebSearch != nil {
		return *e.WebSearch
	}
	return EffectiveWebSearch(e) || IsOfficialDeepSeekSearchEndpoint(e)
}

// ResolveWebSearchProvider freezes one search route for a runtime assembly.
// Prefer the selected chat account; otherwise use the first enabled configured
// search account. An explicit disable on a search-capable current account wins
// over fallback. Exact official routes use the source-rich Messages API.
// Compatible providers keep their own endpoint, credentials and model.
func (c *Config) resolveAutomaticWebSearchProvider(current *ProviderEntry) *ProviderEntry {
	if c == nil {
		return nil
	}
	if current != nil && current.WebSearch != nil && !*current.WebSearch &&
		(SupportsServerWebSearch(current) || IsOfficialDeepSeekSearchEndpoint(current)) {
		return nil
	}
	resolve := func(e *ProviderEntry) *ProviderEntry {
		if !EffectiveIndependentWebSearch(e) || !e.Configured() || e.Model == "" {
			return nil
		}
		copy := cloneProviderEntry(*e)
		if IsOfficialDeepSeekSearchEndpoint(e) {
			copy.Kind = "anthropic"
			copy.BaseURL = "https://api.deepseek.com/anthropic"
			copy.RequestURL = ""
			copy.ChatURL = ""
			copy.ReasoningProtocol = ReasoningProtocolDeepSeek
		}
		copy.WebSearch = boolPointer(true)
		copy.ResponsesStateful = boolPointer(false)
		copy.ResponsesMode = "stateless"
		return &copy
	}
	if selected := resolve(current); selected != nil {
		return selected
	}
	for i := range c.Providers {
		entry, ok := c.ResolveModel(c.Providers[i].Name)
		if ok {
			if selected := resolve(entry); selected != nil {
				return selected
			}
		}
	}
	return nil
}

// WebSearchResolution distinguishes an invalid explicit assignment from automatic
// absence. Entry is a detached route frozen for one runtime.
type WebSearchResolution struct {
	Entry  *ProviderEntry
	Status string
	Reason string
}

// ResolveWebSearchModel resolves an exact account/model without legacy account
// retargeting: an explicit search assignment must never silently change accounts.
func (c *Config) ResolveWebSearchModel(ref string) (*ProviderEntry, error) {
	name, model, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok {
		return nil, fmt.Errorf("search model must use provider/model")
	}
	if c.Desktop.ProviderAccess != nil && !slices.Contains(c.Desktop.ProviderAccess, name) {
		return nil, fmt.Errorf("search connection is not added")
	}
	e, found := c.Provider(name)
	if !found || !e.HasModel(model) {
		return nil, fmt.Errorf("search model is no longer available")
	}
	cp := cloneProviderEntry(*e)
	cp.Model = model
	cp.applyModelPrice()
	cp.applyModelOverride()
	if !SupportsServerWebSearch(&cp) && !IsOfficialDeepSeekSearchEndpoint(&cp) {
		return nil, fmt.Errorf("connection does not support the native search protocol")
	}
	if !EffectiveIndependentWebSearch(&cp) {
		return nil, fmt.Errorf("search is disabled on this connection")
	}
	if !cp.Configured() {
		return nil, fmt.Errorf("search connection has no credentials")
	}
	return &cp, nil
}

func (c *Config) SetWebSearchModel(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.EqualFold(ref, "auto") {
		c.Agent.WebSearchModel = "auto"
		return nil
	}
	e, err := c.ResolveWebSearchModel(ref)
	if err != nil {
		return err
	}
	c.Agent.WebSearchModel = e.Name + "/" + e.Model
	return nil
}

func (c *Config) ResolveWebSearch(current *ProviderEntry) WebSearchResolution {
	if c == nil {
		return WebSearchResolution{Status: "unavailable"}
	}
	if c.Environment.Offline || (len(c.Tools.Enabled) > 0 && !slices.Contains(c.Tools.Enabled, "web_search")) {
		return WebSearchResolution{Status: "disabled"}
	}
	ref := strings.TrimSpace(c.Agent.WebSearchModel)
	if ref != "" && !strings.EqualFold(ref, "auto") {
		entry, err := c.ResolveWebSearchModel(ref)
		if err != nil {
			return WebSearchResolution{Status: "invalid", Reason: err.Error()}
		}
		route := c.resolveAutomaticWebSearchProvider(entry)
		return WebSearchResolution{Entry: route, Status: "ready"}
	}
	if current != nil && current.WebSearch != nil && !*current.WebSearch && (SupportsServerWebSearch(current) || IsOfficialDeepSeekSearchEndpoint(current)) {
		return WebSearchResolution{Status: "disabled"}
	}
	if entry := c.resolveAutomaticWebSearchProvider(current); entry != nil {
		return WebSearchResolution{Entry: entry, Status: "ready"}
	}
	return WebSearchResolution{Status: "unavailable"}
}

// ResolveWebSearchProvider retains the existing API for consumers that only need
// the route. Runtime registration uses ResolveWebSearch to surface invalid refs.
func (c *Config) ResolveWebSearchProvider(current *ProviderEntry) *ProviderEntry {
	return c.ResolveWebSearch(current).Entry
}
