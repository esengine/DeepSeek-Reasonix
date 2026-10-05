// global_presets.go — curated connections for international multi-model services.
package config

var (
	openRouterModels = []string{
		"deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-pro",
		"openai/gpt-6-sol", "google/gemini-3.8-flash",
		"moonshotai/kimi-k3", "z-ai/glm-5.2", "qwen/qwen3.7-max", "minimax/minimax-m3",
	}
	openRouterVisionModels = []string{
		"openai/gpt-6-sol", "google/gemini-3.8-flash",
		"moonshotai/kimi-k3", "minimax/minimax-m3",
	}
	openAIModels       = []string{"gpt-6-sol", "gpt-6-astra", "gpt-6-luna"}
	openAIWindow       = 1_050_000
	geminiModels       = []string{"gemini-3.8-flash", "gemini-3.7-flash", "gemini-3.5-flash-lite"}
	volcengineArkModel = "ark-code-latest"
)

// OpenRouter credits usage to the app named by HTTP-Referer; without it the
// traffic is anonymous in its rankings.
func openRouterAttributionHeaders() map[string]string {
	return map[string]string{
		"HTTP-Referer":            "https://reasonix.io",
		"X-OpenRouter-Title":      "Reasonix",
		"X-OpenRouter-Categories": "cli-agent",
	}
}

var globalProviderPresets = []ProviderPreset{
	{
		ID:          "openrouter",
		Label:       "OpenRouter",
		Description: "OpenRouter multi-model gateway: DeepSeek, GPT, Gemini, Kimi, GLM, Qwen and MiniMax behind one key.",
		KeyEnv:      "OPENROUTER_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "openrouter",
			Kind:          "openai",
			BaseURL:       "https://openrouter.ai/api/v1",
			Models:        openRouterModels,
			VisionModels:  openRouterVisionModels,
			Default:       "deepseek/deepseek-v4-flash",
			APIKeyEnv:     "OPENROUTER_API_KEY",
			BalanceURL:    "https://openrouter.ai/api/v1/credits",
			ContextWindow: 1_000_000,
			Headers:       openRouterAttributionHeaders(),
		}},
	},
	{
		ID:          "openai",
		Label:       "OpenAI",
		Description: "OpenAI Responses API for the GPT-6 family.",
		KeyEnv:      "OPENAI_API_KEY",
		// Responses, because on Chat Completions GPT-6 calls tools only at
		// reasoning_effort=none and every turn here carries tools.
		Entries: []ProviderEntry{{
			Name:             "openai",
			Kind:             kindResponses,
			BaseURL:          "https://api.openai.com/v1",
			Models:           openAIModels,
			VisionModels:     openAIModels,
			Default:          "gpt-6-sol",
			APIKeyEnv:        "OPENAI_API_KEY",
			ContextWindow:    openAIWindow,
			SupportedEfforts: []string{"low", "medium", "high", "xhigh", "max"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "gemini",
		Label:       "Google Gemini",
		Description: "Gemini Developer API through its OpenAI-compatible endpoint.",
		KeyEnv:      "GEMINI_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "gemini",
			Kind:          "openai",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta/openai",
			Models:        geminiModels,
			VisionModels:  geminiModels,
			Default:       "gemini-3.8-flash",
			APIKeyEnv:     "GEMINI_API_KEY",
			ContextWindow: 1_048_576,
		}},
	},
	{
		ID:          "volcengine-coding-plan",
		Label:       "Volcengine Ark Coding Plan",
		Description: "Volcengine Ark Coding Plan (OpenAI-compatible); ark-code-latest follows the model chosen in the Ark console.",
		KeyEnv:      "ARK_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "volcengine-coding-plan",
			Kind:      "openai",
			BaseURL:   "https://ark.cn-beijing.volces.com/api/coding/v3",
			Models:    []string{volcengineArkModel},
			Default:   volcengineArkModel,
			APIKeyEnv: "ARK_API_KEY",
		}},
	},
}
