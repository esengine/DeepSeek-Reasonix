package config

import "strings"

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
func (c *Config) ResolveWebSearchProvider(current *ProviderEntry) *ProviderEntry {
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
