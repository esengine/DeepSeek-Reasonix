package config

import (
	"net/url"
	"strings"
)

// SupportsServerWebSearch reports whether the provider kind has a wire format
// for a provider-executed web search tool. OpenAI Chat Completions is excluded:
// DeepSeek's documented chat-completions tool contract only supports functions.
func SupportsServerWebSearch(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(e.Kind)) {
	case "anthropic", "responses":
		return true
	default:
		return false
	}
}

// IsOfficialDeepSeekWebSearchEndpoint matches the exact protocol base URLs
// documented by DeepSeek. Host-only matching is intentionally insufficient:
// Anthropic requires the /anthropic prefix, while Responses uses the origin.
func IsOfficialDeepSeekWebSearchEndpoint(e *ProviderEntry) bool {
	if !SupportsServerWebSearch(e) {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(e.BaseURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "api.deepseek.com") ||
		u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch strings.ToLower(strings.TrimSpace(e.Kind)) {
	case "responses":
		return path == ""
	case "anthropic":
		return path == "/anthropic"
	default:
		return false
	}
}

// IsOpenCodeGoDeepSeekWebSearchEndpoint matches the exact OpenCode Go routes
// and model set verified to execute provider-side search. The capability is
// model-scoped because the existing Anthropic preset also contains Qwen and
// MiniMax models whose server-tool behavior is independent and not implied by
// the shared endpoint.
func IsOpenCodeGoDeepSeekWebSearchEndpoint(e *ProviderEntry) bool {
	if !SupportsServerWebSearch(e) {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(e.BaseURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "opencode.ai") ||
		u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	models := e.ModelList()
	if len(models) != 1 || !strings.EqualFold(strings.TrimSpace(models[0]), "deepseek-v4-flash") {
		return false
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	switch strings.ToLower(strings.TrimSpace(e.Kind)) {
	case "responses":
		return path == "/zen/go/v1"
	case "anthropic":
		return path == "/zen/go"
	default:
		return false
	}
}

// HasServerWebSearchCapability reports backend-verified endpoint/model pairs
// that the Desktop can safely expose as a provider-side web-search toggle.
func HasServerWebSearchCapability(e *ProviderEntry) bool {
	return IsOfficialDeepSeekWebSearchEndpoint(e) || IsOpenCodeGoDeepSeekWebSearchEndpoint(e)
}

// EffectiveWebSearch resolves the persisted tri-state. Official DeepSeek
// Anthropic and Responses endpoints default on when old configuration omitted
// web_search, while compatible third-party endpoints remain opt-in. An explicit
// false always wins so users can turn the capability off permanently.
func EffectiveWebSearch(e *ProviderEntry) bool {
	if !SupportsServerWebSearch(e) {
		return false
	}
	if e.WebSearch != nil {
		return *e.WebSearch
	}
	return IsOfficialDeepSeekWebSearchEndpoint(e)
}
