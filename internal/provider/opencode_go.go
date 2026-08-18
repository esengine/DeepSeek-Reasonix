package provider

import (
	"net/url"
	"strings"
)

// Official OpenCode Go routes verified against the public endpoint list.
// Similar hostnames, custom proxies, and extra query/userinfo never match.
const (
	openCodeGoHost          = "opencode.ai"
	openCodeGoChatPath      = "/zen/go/v1"
	openCodeGoAnthropicPath = "/zen/go"
)

// OpenCodeGoModelLimits is the static models.dev snapshot used by both config
// presets and provider policy. There is no runtime network fetch.
type OpenCodeGoModelLimits struct {
	Context   int
	MaxOutput int
}

const (
	OpenCodeGoRouteChat      = "chat"
	OpenCodeGoRouteAnthropic = "anthropic"
	OpenCodeGoRouteResponses = "responses"
)

// OpenCodeGoChatModels is the official Chat Completions catalog.
func OpenCodeGoChatModels() map[string]OpenCodeGoModelLimits {
	return map[string]OpenCodeGoModelLimits{
		"glm-5.2":           {Context: 1_000_000, MaxOutput: 131_072},
		"glm-5.1":           {Context: 202_752, MaxOutput: 32_768},
		"kimi-k3":           {Context: 1_048_576, MaxOutput: 131_072},
		"kimi-k2.7-code":    {Context: 262_144, MaxOutput: 262_144},
		"kimi-k2.6":         {Context: 262_144, MaxOutput: 65_536},
		"deepseek-v4-pro":   {Context: 1_000_000, MaxOutput: 384_000},
		"deepseek-v4-flash": {Context: 1_000_000, MaxOutput: 384_000},
		"mimo-v2.5-pro":     {Context: 1_048_576, MaxOutput: 128_000},
		"mimo-v2.5":         {Context: 1_000_000, MaxOutput: 128_000},
	}
}

// OpenCodeGoAnthropicModels is the official Anthropic-compatible catalog.
func OpenCodeGoAnthropicModels() map[string]OpenCodeGoModelLimits {
	return map[string]OpenCodeGoModelLimits{
		"qwen3.7-max":  {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.7-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.6-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"minimax-m3":   {Context: 1_000_000, MaxOutput: 131_072},
		"minimax-m2.7": {Context: 204_800, MaxOutput: 131_072},
		"minimax-m2.5": {Context: 204_800, MaxOutput: 65_536},
	}
}

func officialOpenCodeGoPath(baseURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), openCodeGoHost) ||
		u.Port() != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimRight(u.EscapedPath(), "/"), true
}

// OfficialOpenCodeGoRoute reports the exact official route for kind+URL.
// kind is openai/chat, anthropic, or responses. Unknown kind/URL is false.
func OfficialOpenCodeGoRoute(kind, baseURL string) (string, bool) {
	path, ok := officialOpenCodeGoPath(baseURL)
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai", "chat", "":
		if path == openCodeGoChatPath {
			return OpenCodeGoRouteChat, true
		}
	case "responses":
		if path == openCodeGoChatPath {
			return OpenCodeGoRouteResponses, true
		}
	case "anthropic":
		if path == openCodeGoAnthropicPath {
			return OpenCodeGoRouteAnthropic, true
		}
	}
	return "", false
}

func lookupOpenCodeGoLimits(table map[string]OpenCodeGoModelLimits, model string) (OpenCodeGoModelLimits, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return OpenCodeGoModelLimits{}, false
	}
	for id, lim := range table {
		if strings.ToLower(id) == model {
			return lim, true
		}
	}
	return OpenCodeGoModelLimits{}, false
}

func isDeepSeekModelID(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "deepseek-")
}

// LookupOfficialOpenCodeGo returns static limits only for the official
// endpoint and a model listed for that route. Future/unknown models miss.
func LookupOfficialOpenCodeGo(kind, baseURL, model string) (OpenCodeGoModelLimits, bool) {
	route, ok := OfficialOpenCodeGoRoute(kind, baseURL)
	if !ok {
		return OpenCodeGoModelLimits{}, false
	}
	switch route {
	case OpenCodeGoRouteChat:
		return lookupOpenCodeGoLimits(OpenCodeGoChatModels(), model)
	case OpenCodeGoRouteAnthropic:
		if lim, ok := lookupOpenCodeGoLimits(OpenCodeGoAnthropicModels(), model); ok {
			return lim, true
		}
		if isDeepSeekModelID(model) {
			return lookupOpenCodeGoLimits(OpenCodeGoChatModels(), model)
		}
	case OpenCodeGoRouteResponses:
		if isDeepSeekModelID(model) {
			return lookupOpenCodeGoLimits(OpenCodeGoChatModels(), model)
		}
	}
	return OpenCodeGoModelLimits{}, false
}
