package config

import "reasonix/internal/provider"

func withOpenCodeGoChatContextOverrides(base map[string]ProviderModelOverride) map[string]ProviderModelOverride {
	if base == nil {
		base = map[string]ProviderModelOverride{}
	}
	for id, lim := range provider.OpenCodeGoChatModels() {
		ov := base[id]
		ov.ContextWindow = lim.Context
		base[id] = ov
	}
	return base
}

func openCodeGoAnthropicContextOverrides() map[string]ProviderModelOverride {
	out := map[string]ProviderModelOverride{}
	for id, lim := range provider.OpenCodeGoAnthropicModels() {
		out[id] = ProviderModelOverride{ContextWindow: lim.Context}
	}
	return out
}

var opencodeGoAnthropicPreset = ProviderPreset{
	ID:          "opencode-go-anthropic",
	Label:       "OpenCode Go Anthropic",
	Description: "OpenCode Go subscription Anthropic-compatible route for Qwen and MiniMax models.",
	KeyEnv:      "OPENCODE_GO_API_KEY",
	Entries: []ProviderEntry{{
		Name:           "opencode-go-anthropic",
		Kind:           "anthropic",
		BaseURL:        "https://opencode.ai/zen/go",
		Models:         []string{"qwen3.7-plus", "qwen3.7-max", "qwen3.6-plus", "minimax-m3", "minimax-m2.7", "minimax-m2.5"},
		VisionModels:   []string{"qwen3.7-plus", "qwen3.6-plus"},
		Default:        "qwen3.7-plus",
		APIKeyEnv:      "OPENCODE_GO_API_KEY",
		Thinking:       "adaptive",
		ContextWindow:  262144,
		ModelOverrides: openCodeGoAnthropicContextOverrides(),
	}},
}

var opencodeGoDeepSeekAnthropicPreset = ProviderPreset{
	ID:          "opencode-go-deepseek-anthropic",
	Label:       "OpenCode Go DeepSeek Anthropic",
	Description: "OpenCode Go Anthropic Messages route for DeepSeek Flash with server-side web search.",
	KeyEnv:      "OPENCODE_GO_API_KEY",
	Entries: []ProviderEntry{{
		Name:             "opencode-go-deepseek-anthropic",
		Kind:             "anthropic",
		BaseURL:          "https://opencode.ai/zen/go",
		Models:           []string{"deepseek-v4-flash"},
		Default:          "deepseek-v4-flash",
		APIKeyEnv:        "OPENCODE_GO_API_KEY",
		Thinking:         "adaptive",
		WebSearch:        boolPointer(true),
		ContextWindow:    1_000_000,
		SupportedEfforts: []string{"disabled", "high", "max"},
		DefaultEffort:    "high",
	}},
}

var opencodeGoDeepSeekResponsesPreset = ProviderPreset{
	ID:          "opencode-go-deepseek-responses",
	Label:       "OpenCode Go DeepSeek Responses",
	Description: "OpenCode Go stateless Responses API route for DeepSeek Flash with server-side web search.",
	KeyEnv:      "OPENCODE_GO_API_KEY",
	Entries: []ProviderEntry{{
		Name:             "opencode-go-deepseek-responses",
		Kind:             "responses",
		BaseURL:          "https://opencode.ai/zen/go/v1",
		Models:           []string{"deepseek-v4-flash"},
		Default:          "deepseek-v4-flash",
		APIKeyEnv:        "OPENCODE_GO_API_KEY",
		ResponsesMode:    "stateless",
		WebSearch:        boolPointer(true),
		ContextWindow:    1_000_000,
		SupportedEfforts: []string{"disabled", "high", "max"},
		DefaultEffort:    "high",
	}},
}
