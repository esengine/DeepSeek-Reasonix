package provider

import (
	"maps"
	"slices"
	"strings"
)

// OpenCodeGoContract separates the recommended route from a supported alternate
// route. It is a local, versioned contract: discovery never performs network I/O.
type OpenCodeGoContract struct {
	Model             string
	Route             string
	RecommendedRoute  string
	ReasoningProtocol string
	Thinking          string
	Reasoning         ReasoningCapability
}

// OpenCodeGoRequestRoute recognizes the effective request endpoint, including
// the exact URLs saved by Desktop. Custom overrides must never inherit facts
// from an unrelated base URL.
func OpenCodeGoRequestRoute(kind, baseURL, requestURL, chatURL string) (string, bool) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	exact := strings.TrimSpace(requestURL)
	if exact == "" && (kind == "openai" || kind == "chat") {
		exact = strings.TrimSpace(chatURL)
	}
	if exact != "" {
		path, ok := officialOpenCodeGoPath(exact)
		if !ok {
			return "", false
		}
		switch {
		case (kind == "openai" || kind == "chat") && path == "/zen/go/v1/chat/completions":
			return OpenCodeGoRouteChat, true
		case kind == "anthropic" && path == "/zen/go/v1/messages":
			return OpenCodeGoRouteAnthropic, true
		case kind == "responses" && path == "/zen/go/v1/responses":
			return OpenCodeGoRouteResponses, true
		default:
			return "", false
		}
	}
	if kind == "anthropic" {
		baseURL = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(baseURL), "/"), "/v1")
	}
	return OfficialOpenCodeGoRoute(kind, baseURL)
}

// OpenCodeGoRecommendedRoute uses the pinned Pi catalog first. The compatibility
// catalog only fills absent IDs; alternate DeepSeek routes are not recommendations.
func OpenCodeGoRecommendedRoute(model string) (string, bool) {
	model = strings.TrimSpace(model)
	if OpenCodeGoDeepSeekModel(model) {
		return OpenCodeGoRouteChat, true
	}
	for _, route := range []string{OpenCodeGoRouteChat, OpenCodeGoRouteAnthropic, OpenCodeGoRouteResponses} {
		if slices.Contains(PiCatalogOpenCodeGoModelIDs(route), model) {
			return route, true
		}
	}
	for _, item := range []struct {
		route  string
		models map[string]OpenCodeGoModelLimits
	}{{OpenCodeGoRouteChat, OpenCodeGoChatModels()}, {OpenCodeGoRouteAnthropic, OpenCodeGoAnthropicModels()}, {OpenCodeGoRouteResponses, OpenCodeGoResponsesModels()}} {
		if _, ok := item.models[model]; ok {
			return item.route, true
		}
	}
	return "", false
}

func OpenCodeGoDeepSeekModel(model string) bool {
	switch strings.TrimSpace(model) {
	case "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp":
		return true
	}
	return false
}

func LookupOpenCodeGoContract(kind, baseURL, requestURL, chatURL, model string) (OpenCodeGoContract, bool) {
	route, ok := OpenCodeGoRequestRoute(kind, baseURL, requestURL, chatURL)
	if !ok {
		return OpenCodeGoContract{}, false
	}
	return OpenCodeGoContractForRoute(route, model)
}

func OpenCodeGoContractForRoute(route, model string) (OpenCodeGoContract, bool) {
	model = strings.TrimSpace(model)
	recommended, known := OpenCodeGoRecommendedRoute(model)
	if !known {
		return OpenCodeGoContract{}, false
	}
	if route != OpenCodeGoRouteChat && route != OpenCodeGoRouteAnthropic && route != OpenCodeGoRouteResponses {
		return OpenCodeGoContract{}, false
	}
	if _, supported := LookupOfficialOpenCodeGo(routeKind(route), routeURL(route), model); !supported {
		return OpenCodeGoContract{}, false
	}
	c := OpenCodeGoContract{Model: model, Route: route, RecommendedRoute: recommended}
	if OpenCodeGoDeepSeekModel(model) {
		c.RecommendedRoute, c.ReasoningProtocol, c.Thinking = OpenCodeGoRouteChat, "deepseek", "enabled"
		c.Reasoning = ReasoningOptions("high", "disabled", "low", "high", "max")
		if route == OpenCodeGoRouteResponses {
			c.Reasoning = ReasoningOptions("high", "none", "low", "high", "max")
		}
		return c, true
	}
	if route == OpenCodeGoRouteAnthropic {
		c.Thinking = "adaptive"
		c.Reasoning = ReasoningOptions("", "low", "medium", "high", "xhigh", "max")
		return c, true
	}
	levels := map[string][]string{
		"glm-5.3": {"low", "high", "max"}, "glm-5.2": {"high", "max"}, "glm-5.1": {"low", "high"},
		"kimi-k3": {"high", "max"}, "kimi-k2.7-code": {"low", "medium", "high"}, "kimi-k2.6": {"low", "medium", "high"},
		"hy3": {"none", "low", "high"}, "grok-4.5": {"low", "medium", "high"},
		"gpt-5.6-luna":               {"none", "low", "medium", "high", "xhigh", "max"},
		"muse-spark-1.2-contributor": {"minimal", "low", "medium", "high", "xhigh"},
	}
	if ids, ok := levels[model]; ok {
		c.ReasoningProtocol = "openai"
		c.Reasoning = ReasoningOptions("high", ids...)
	} else if cap, ok := PiCatalogOpenCodeGoReasoning(route, model); ok {
		c.ReasoningProtocol = "openai"
		c.Reasoning = cap
	}
	return c, true
}

// FilterOpenCodeGoRequestModels is shared by discovery and settings. The
// effective request URL determines the protocol, including exact URL overrides.
func FilterOpenCodeGoRequestModels(kind, baseURL, requestURL, chatURL string, models []string) []string {
	route, ok := OpenCodeGoRequestRoute(kind, baseURL, requestURL, chatURL)
	if !ok {
		return models
	}
	out := make([]string, 0, len(models))
	for _, model := range models {
		if _, supported := OpenCodeGoContractForRoute(route, model); supported {
			out = append(out, model)
		}
	}
	return out
}

// ApplyOpenCodeGoContract provides identical inputs to capability validation and
// serialization. Explicit declarations still win; migrations repair old defaults.
func ApplyOpenCodeGoContract(kind string, cfg Config) Config {
	requestURL, _ := cfg.Extra["request_url"].(string)
	chatURL, _ := cfg.Extra["chat_url"].(string)
	c, ok := LookupOpenCodeGoContract(kind, cfg.BaseURL, requestURL, chatURL, cfg.Model)
	if !ok {
		return cfg
	}
	cfg.Extra = maps.Clone(cfg.Extra)
	if cfg.Extra == nil {
		cfg.Extra = map[string]any{}
	}
	protocol, _ := cfg.Extra["reasoning_protocol"].(string)
	if strings.TrimSpace(protocol) == "" || protocol == "auto" {
		cfg.Extra["reasoning_protocol"] = c.ReasoningProtocol
	}
	if (protocol == "" || protocol == "auto" || protocol == c.ReasoningProtocol) && len(c.Reasoning.Options) > 0 {
		if ids, _ := cfg.Extra["supported_efforts"].([]string); len(ids) == 0 {
			cfg.Extra["supported_efforts"] = c.Reasoning.IDs()
			if def, _ := cfg.Extra["default_effort"].(string); def == "" {
				cfg.Extra["default_effort"] = c.Reasoning.Default
			}
		}
	}
	return cfg
}
