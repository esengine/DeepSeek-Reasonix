package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

var ErrWebSearchModelUnavailable = errors.New("web search model unavailable")

type WebSearchResolution struct {
	Entry *ProviderEntry
	Err   error
}

func IsOfficialDeepSeekSearchEndpoint(e *ProviderEntry) bool {
	if e == nil || overridesSearchRoute(e, e.RequestURL) || overridesSearchRoute(e, e.ChatURL) {
		return false
	}
	if IsOfficialDeepSeekWebSearchEndpoint(e) {
		return true
	}
	if !strings.EqualFold(e.Kind, "openai") {
		return false
	}
	cp := *e
	cp.Kind = "responses"
	cp.BaseURL = strings.TrimSuffix(strings.TrimRight(e.BaseURL, "/"), "/v1")
	return IsOfficialDeepSeekWebSearchEndpoint(&cp)
}

func overridesSearchRoute(e *ProviderEntry, raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	explicit, ok := normalizedSearchURL(raw)
	if !ok {
		return true
	}
	base := strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
	var route string
	switch strings.ToLower(strings.TrimSpace(e.Kind)) {
	case "openai":
		route = base + "/chat/completions"
	case "anthropic":
		route = strings.TrimSuffix(base, "/v1") + "/v1/messages"
	case "responses":
		route = base + "/responses"
	}
	derived, ok := normalizedSearchURL(route)
	return !ok || explicit != derived
}

func normalizedSearchURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", false
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), true
}

func EffectiveIndependentWebSearch(e *ProviderEntry) bool {
	if e == nil || (!SupportsServerWebSearch(e) && !IsOfficialDeepSeekSearchEndpoint(e)) {
		return false
	}
	if e.WebSearch != nil {
		return *e.WebSearch
	}
	return EffectiveWebSearch(e) || IsOfficialDeepSeekSearchEndpoint(e)
}

func (c *Config) ResolveWebSearchModel(ref string) (*ProviderEntry, error) {
	name, model, exact := strings.Cut(strings.TrimSpace(ref), "/")
	if c == nil || !exact || name == "" || model == "" {
		return nil, fmt.Errorf("%w: use provider/model", ErrWebSearchModelUnavailable)
	}
	if c.Desktop.ProviderAccess != nil && !slices.Contains(c.Desktop.ProviderAccess, name) {
		return nil, fmt.Errorf("%w: connection is not added", ErrWebSearchModelUnavailable)
	}
	e, found := c.Provider(name)
	if !found || !e.HasModel(model) {
		return nil, fmt.Errorf("%w: model is no longer configured", ErrWebSearchModelUnavailable)
	}
	cp := cloneProviderEntry(*e.forModel(model))
	if !EffectiveIndependentWebSearch(&cp) {
		return nil, fmt.Errorf("%w: native search is unsupported or disabled", ErrWebSearchModelUnavailable)
	}
	if !cp.Configured() {
		return nil, fmt.Errorf("%w: connection has no credentials", ErrWebSearchModelUnavailable)
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

func searchRoute(e *ProviderEntry) *ProviderEntry {
	if !EffectiveIndependentWebSearch(e) || !e.Configured() || e.Model == "" {
		return nil
	}
	cp := cloneProviderEntry(*e)
	if IsOfficialDeepSeekSearchEndpoint(e) {
		cp.Kind, cp.BaseURL = "anthropic", "https://api.deepseek.com/anthropic"
		cp.RequestURL, cp.ChatURL = "", ""
		cp.ReasoningProtocol = ReasoningProtocolDeepSeek
	}
	on, off := true, false
	cp.WebSearch = &on
	cp.ResponsesStateful = &off
	cp.ResponsesMode = "stateless"
	return &cp
}

func (c *Config) ResolveWebSearch(current *ProviderEntry) WebSearchResolution {
	if c == nil || c.Environment.Offline || (len(c.Tools.Enabled) > 0 && !slices.Contains(c.Tools.Enabled, "web_search")) {
		return WebSearchResolution{}
	}
	ref := strings.TrimSpace(c.Agent.WebSearchModel)
	if ref != "" && !strings.EqualFold(ref, "auto") {
		e, err := c.ResolveWebSearchModel(ref)
		if err != nil {
			return WebSearchResolution{Err: err}
		}
		return WebSearchResolution{Entry: searchRoute(e)}
	}
	if current != nil && current.WebSearch != nil && !*current.WebSearch && (SupportsServerWebSearch(current) || IsOfficialDeepSeekSearchEndpoint(current) || openCodeSearchAccount(current)) {
		return WebSearchResolution{}
	}
	if e := searchRoute(current); e != nil {
		return WebSearchResolution{Entry: e}
	}
	for i := range c.Providers {
		e := c.Providers[i].forModel(c.Providers[i].DefaultModel())
		if openCodeSearchAccount(current) && (e.APIKeyEnv != current.APIKeyEnv || !openCodeSearchAccount(e)) {
			continue
		}
		if c.Desktop.ProviderAccess != nil && !slices.Contains(c.Desktop.ProviderAccess, e.Name) {
			continue
		}
		if route := searchRoute(e); route != nil {
			return WebSearchResolution{Entry: route}
		}
	}
	return WebSearchResolution{}
}

func openCodeSearchAccount(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Host, "opencode.ai") {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	return path == "/zen/go" || path == "/zen/go/v1"
}
