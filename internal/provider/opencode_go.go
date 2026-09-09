package provider

import (
	"maps"
	"net/url"
	"sort"
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
	return mergeOpenCodeGoLimits(OpenCodeGoRouteChat, map[string]OpenCodeGoModelLimits{
		"glm-5.3": {Context: 1_000_000, MaxOutput: 131_072}, "glm-5.2": {Context: 1_000_000, MaxOutput: 131_072},
		"glm-5.1": {Context: 202_752, MaxOutput: 32_768}, "kimi-k3": {Context: 1_048_576, MaxOutput: 131_072},
		"kimi-k2.7-code": {Context: 262_144, MaxOutput: 262_144}, "kimi-k2.6": {Context: 262_144, MaxOutput: 65_536},
		"deepseek-v4-pro": {Context: 1_000_000, MaxOutput: 384_000}, "deepseek-v4-flash": {Context: 1_000_000, MaxOutput: 384_000},
		"deepseek-v4-flash-vision-exp": {Context: 1_000_000, MaxOutput: 384_000}, "mimo-v2.5-pro": {Context: 1_048_576, MaxOutput: 128_000},
		"mimo-v2.5": {Context: 1_000_000, MaxOutput: 128_000}, "hy3": {Context: 256_000, MaxOutput: 64_000},
	})
}

// OpenCodeGoAnthropicModels is the official Anthropic-compatible catalog.
func OpenCodeGoAnthropicModels() map[string]OpenCodeGoModelLimits {
	return mergeOpenCodeGoLimits(OpenCodeGoRouteAnthropic, map[string]OpenCodeGoModelLimits{
		"qwen3.8-max": {Context: 1_000_000, MaxOutput: 131_072}, "qwen3.7-max": {Context: 1_000_000, MaxOutput: 65_536},
		"qwen3.7-plus": {Context: 1_000_000, MaxOutput: 65_536}, "qwen3.6-plus": {Context: 1_000_000, MaxOutput: 65_536},
		"minimax-m3": {Context: 1_000_000, MaxOutput: 131_072}, "minimax-m2.7": {Context: 204_800, MaxOutput: 131_072},
		"minimax-m2.5": {Context: 204_800, MaxOutput: 65_536},
	})
}

// OpenCodeGoResponsesModels is the official Responses API catalog. DeepSeek's
// separately verified alternative Responses route remains supported below.
func OpenCodeGoResponsesModels() map[string]OpenCodeGoModelLimits {
	return mergeOpenCodeGoLimits(OpenCodeGoRouteResponses, map[string]OpenCodeGoModelLimits{
		"grok-4.5": {Context: 500_000, MaxOutput: 500_000}, "gpt-5.6-luna": {Context: 1_050_000, MaxOutput: 128_000},
		"muse-spark-1.2-contributor": {Context: 1_048_576, MaxOutput: 131_072},
	})
}

func mergeOpenCodeGoLimits(route string, compatibility map[string]OpenCodeGoModelLimits) map[string]OpenCodeGoModelLimits {
	out := make(map[string]OpenCodeGoModelLimits, len(compatibility))
	maps.Copy(out, compatibility)
	for _, model := range piCatalogOpenCodeGoModels(route) {
		out[model.ID] = OpenCodeGoModelLimits{Context: model.ContextWindow, MaxOutput: model.MaxTokens}
	}
	return out
}

// OpenCodeGoModelIDs returns the union of the embedded Pi catalog and the
// verified compatibility catalog, preserving models still served by the
// endpoint even if one catalog snapshot lags the other.
func OpenCodeGoModelIDs(route string) []string {
	var models map[string]OpenCodeGoModelLimits
	switch route {
	case OpenCodeGoRouteChat:
		models = OpenCodeGoChatModels()
	case OpenCodeGoRouteAnthropic:
		models = OpenCodeGoAnthropicModels()
	case OpenCodeGoRouteResponses:
		models = OpenCodeGoResponsesModels()
	}
	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func OpenCodeGoVisionModelIDs(route string) []string {
	ids := OpenCodeGoModelIDs(route)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if info, ok := OpenCodeGoModelInfo(routeKind(route), routeURL(route), id); ok && info.SupportsInput(ModalityImage) {
			out = append(out, id)
		}
	}
	return out
}

func routeKind(route string) string {
	switch route {
	case OpenCodeGoRouteAnthropic:
		return "anthropic"
	case OpenCodeGoRouteResponses:
		return "responses"
	default:
		return "openai"
	}
}

func routeURL(route string) string {
	if route == OpenCodeGoRouteAnthropic {
		return "https://opencode.ai/zen/go"
	}
	return "https://opencode.ai/zen/go/v1"
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
		if lim, ok := lookupOpenCodeGoLimits(OpenCodeGoResponsesModels(), model); ok {
			return lim, true
		}
		if isDeepSeekModelID(model) {
			return lookupOpenCodeGoLimits(OpenCodeGoChatModels(), model)
		}
	}
	return OpenCodeGoModelLimits{}, false
}

// OpenCodeGoModelInfo returns the adapter-owned model metadata for the exact
// official OpenCode Go route. It intentionally returns text-only for every
// listed model unless the local adapter has an explicit image declaration.
func OpenCodeGoModelInfo(kind, baseURL, model string) (ModelInfo, bool) {
	if info, ok := PiCatalogModelInfo(kind, baseURL, model); ok {
		return info, true
	}
	route, ok := OfficialOpenCodeGoRoute(kind, baseURL)
	if !ok {
		return ModelInfo{}, false
	}
	if _, ok := LookupOfficialOpenCodeGo(kind, baseURL, model); !ok {
		return ModelInfo{}, false
	}
	info := ModelInfo{ID: strings.TrimSpace(model), InputModalities: []ModelModality{ModalityText}}
	vision := map[string]map[string]bool{
		OpenCodeGoRouteChat:      {"kimi-k3": true},
		OpenCodeGoRouteAnthropic: {"qwen3.8-max": true, "qwen3.7-plus": true, "qwen3.6-plus": true},
		OpenCodeGoRouteResponses: {"grok-4.5": true, "gpt-5.6-luna": true, "muse-spark-1.2-contributor": true},
	}[route]
	if vision[strings.ToLower(strings.TrimSpace(model))] {
		info.InputModalities = []ModelModality{ModalityText, ModalityImage}
	}
	return info, true
}

// BuiltinModelInfo is the shared local catalog seam for built-in adapters.
// It is deliberately exact-match and returns false for unknown/custom routes.
func BuiltinModelInfo(kind, baseURL, model string) (ModelInfo, bool) {
	if info, ok := OpenCodeGoModelInfo(kind, baseURL, model); ok {
		return info, true
	}
	if info, ok := ModelScopeModelInfo(kind, baseURL, model); ok {
		return info, true
	}
	if strings.EqualFold(strings.TrimSpace(kind), "openai") || strings.EqualFold(strings.TrimSpace(kind), "responses") || strings.EqualFold(strings.TrimSpace(kind), "anthropic") {
		u, err := url.Parse(strings.TrimSpace(baseURL))
		if err == nil && strings.EqualFold(u.Scheme, "https") && strings.EqualFold(u.Hostname(), "api.deepseek.com") {
			id := strings.TrimSpace(model)
			if id == "deepseek-v4-flash-vision-exp" {
				return ModelInfo{ID: id, InputModalities: []ModelModality{ModalityText, ModalityImage}}, true
			}
			if id == "deepseek-v4-flash" || id == "deepseek-v4-pro" {
				return ModelInfo{ID: id, InputModalities: []ModelModality{ModalityText}}, true
			}
		}
	}
	return ModelInfo{}, false
}

// ModelScopeModelInfo is the local catalog for the curated ModelScope route.
// The endpoint currently exposes an OpenAI-compatible API without reliable
// capability fields, so only the verified Qwen3.5 SKUs are image-capable.
func ModelScopeModelInfo(kind, baseURL, model string) (ModelInfo, bool) {
	if !strings.EqualFold(strings.TrimSpace(kind), "openai") {
		return ModelInfo{}, false
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "api-inference.modelscope.cn") || u.Port() != "" {
		return ModelInfo{}, false
	}
	known := map[string]bool{
		"qwen/qwen3.5-397b-a17b":             true,
		"qwen/qwen3.5-122b-a10b":             true,
		"qwen/qwen3.5-27b":                   true,
		"deepseek-ai/deepseek-v4-flash-0731": false,
		"deepseek-ai/deepseek-v4-pro":        false,
		"minimax/minimax-m3":                 false,
		"zhipuai/glm-5.2":                    false,
	}
	id := strings.TrimSpace(model)
	vision, ok := known[strings.ToLower(id)]
	if !ok {
		return ModelInfo{}, false
	}
	modalities := []ModelModality{ModalityText}
	if vision {
		modalities = append(modalities, ModalityImage)
	}
	return ModelInfo{ID: id, InputModalities: modalities}, true
}

// FilterOfficialOpenCodeGoModels removes models that the shared OpenCode Go
// /v1/models catalog exposes for a different wire format. Custom endpoints are
// returned unchanged because Reasonix cannot infer their routing policy.
func FilterOfficialOpenCodeGoModels(kind, baseURL string, models []string) []string {
	if _, ok := OfficialOpenCodeGoRoute(kind, baseURL); !ok {
		return models
	}
	out := make([]string, 0, len(models))
	for _, model := range models {
		if _, ok := LookupOfficialOpenCodeGo(kind, baseURL, model); ok {
			out = append(out, model)
		}
	}
	return out
}
