package config

import (
	"errors"
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
	return declaredWebSearch(e) || IsOfficialDeepSeekSearchEndpoint(e)
}

// WebSearchReason says why an explicitly selected search model cannot serve.
type WebSearchReason string

const (
	WebSearchBadRef        WebSearchReason = "bad_ref"
	WebSearchNotAdded      WebSearchReason = "not_added"
	WebSearchModelRemoved  WebSearchReason = "model_removed"
	WebSearchUnsupported   WebSearchReason = "unsupported"
	WebSearchNoCredentials WebSearchReason = "no_credentials"
)

// WebSearchUnavailableError carries the reason; it matches ErrWebSearchModelUnavailable.
type WebSearchUnavailableError struct{ Reason WebSearchReason }

func (e *WebSearchUnavailableError) Error() string {
	return "web search model unavailable: " + string(e.Reason)
}

func (e *WebSearchUnavailableError) Is(target error) bool {
	return target == ErrWebSearchModelUnavailable
}

func WebSearchReasonOf(err error) (WebSearchReason, bool) {
	var u *WebSearchUnavailableError
	if errors.As(err, &u) {
		return u.Reason, true
	}
	return "", false
}

func (c *Config) ResolveWebSearchModel(ref string) (*ProviderEntry, error) {
	name, model, exact := strings.Cut(strings.TrimSpace(ref), "/")
	if c == nil || !exact || name == "" || model == "" {
		return nil, &WebSearchUnavailableError{WebSearchBadRef}
	}
	if c.Desktop.ProviderAccess != nil && !slices.Contains(c.Desktop.ProviderAccess, name) {
		return nil, &WebSearchUnavailableError{WebSearchNotAdded}
	}
	e, found := c.Provider(name)
	if !found || !e.HasModel(model) {
		return nil, &WebSearchUnavailableError{WebSearchModelRemoved}
	}
	cp := cloneProviderEntry(*e.forModel(model))
	if !EffectiveIndependentWebSearch(&cp) {
		return nil, &WebSearchUnavailableError{WebSearchUnsupported}
	}
	if !cp.Configured() {
		return nil, &WebSearchUnavailableError{WebSearchNoCredentials}
	}
	return &cp, nil
}

func (c *Config) SetWebSearchModel(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.EqualFold(ref, "auto") {
		c.Agent.WebSearchModel = ""
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

// ResolveWebSearch answers only for an explicit selection. Unset or "auto"
// leaves search to the conversation provider's own built-in tool, so the
// account that serves the conversation is the only one that sees a query.
func (c *Config) ResolveWebSearch() WebSearchResolution {
	if c == nil || c.Environment.Offline || (len(c.Tools.Enabled) > 0 && !slices.Contains(c.Tools.Enabled, "web_search")) {
		return WebSearchResolution{}
	}
	ref := c.explicitWebSearchRef()
	if ref == "" {
		return WebSearchResolution{}
	}
	e, err := c.ResolveWebSearchModel(ref)
	if err != nil {
		return WebSearchResolution{Err: err}
	}
	return WebSearchResolution{Entry: searchRoute(e)}
}

func (c *Config) explicitWebSearchRef() string {
	ref := strings.TrimSpace(c.Agent.WebSearchModel)
	if strings.EqualFold(ref, "auto") {
		return ""
	}
	return ref
}

// WebSearchOutcome is what search will actually use: the explicit selection, or
// under auto the default conversation model when its provider searches natively.
type WebSearchOutcome struct {
	Ref    string
	Auto   bool
	Reason WebSearchReason
}

func (c *Config) WebSearchOutcome() WebSearchOutcome {
	if ref := c.explicitWebSearchRef(); ref != "" {
		e, err := c.ResolveWebSearchModel(ref)
		if err != nil {
			reason, _ := WebSearchReasonOf(err)
			return WebSearchOutcome{Reason: reason}
		}
		return WebSearchOutcome{Ref: e.Name + "/" + e.Model}
	}
	out := WebSearchOutcome{Auto: true}
	if resolved, _, ok := c.ResolveModelWithFallback(c.DefaultModel); ok {
		if e, found := c.ResolveModel(resolved); found && EffectiveWebSearch(e) {
			out.Ref = e.Name + "/" + e.Model
		}
	}
	return out
}
