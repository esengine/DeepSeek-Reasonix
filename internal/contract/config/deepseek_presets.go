// deepseek_presets.go — the curated connections for DeepSeek's own endpoints.
package config

// deepSeekOfficialModels is what the official endpoint serves today; the Responses
// shape carries flash alone, which is the only model it documents.
var (
	deepSeekOfficialModels  = []string{DeepSeekFlashModel, deepSeekProModel}
	deepSeekResponsesModels = []string{DeepSeekFlashModel}
)

func deepSeekAnthropicPreset() ProviderPreset {
	return ProviderPreset{
		ID:          "deepseek-anthropic",
		Label:       "DeepSeek Official Anthropic",
		Description: "Separate official DeepSeek Anthropic-compatible entry for Flash and Pro.",
		KeyEnv:      "DEEPSEEK_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "deepseek-anthropic",
			Kind:          "anthropic",
			BaseURL:       deepSeekAnthropicBaseURL,
			Models:        deepSeekOfficialModels,
			Default:       DeepSeekFlashModel,
			VisionModels:  []string{DeepSeekFlashModel},
			APIKeyEnv:     "DEEPSEEK_API_KEY",
			BalanceURL:    "https://api.deepseek.com/user/balance",
			Thinking:      "enabled",
			WebSearch:     new(true),
			ContextWindow: 1_000_000,
			Prices:        deepSeekOfficialPricesUSD(),
			ModelOverrides: map[string]ProviderModelOverride{
				DeepSeekFlashModel: {SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "high"},
				deepSeekProModel:   {SupportedEfforts: []string{"disabled", "high", "max"}, DefaultEffort: "high"},
			},
		}},
	}
}

// Images ride input_image parts here, measured 2026-09-13 against the endpoint.
func deepSeekResponsesPreset() ProviderPreset {
	return ProviderPreset{
		ID:          "deepseek-responses",
		Label:       "DeepSeek Official Responses API",
		Description: "Official stateless DeepSeek Responses API for Flash with server-side web search.",
		KeyEnv:      "DEEPSEEK_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "deepseek-responses",
			Kind:             "responses",
			BaseURL:          "https://api.deepseek.com",
			Models:           deepSeekResponsesModels,
			Default:          DeepSeekFlashModel,
			VisionModels:     []string{DeepSeekFlashModel},
			APIKeyEnv:        "DEEPSEEK_API_KEY",
			BalanceURL:       "https://api.deepseek.com/user/balance",
			ContextWindow:    1_000_000,
			Price:            deepSeekOfficialRate(DeepSeekFlashModel, "USD"),
			ResponsesMode:    "stateless",
			WebSearch:        new(true),
			SupportedEfforts: []string{"low", "high", "max"},
			DefaultEffort:    "high",
		}},
	}
}
