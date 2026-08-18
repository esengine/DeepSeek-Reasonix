package config

var scnetOpenAIModels = []string{
	"GLM-5.2",
	"GLM-5",
	"GLM-5.1",
	"Kimi-K3",
	"Kimi-K2.7-Code",
	"Kimi-K2.6",
	"Kimi-K2.5",
	"DeepSeek-V4-Flash",
	"DeepSeek-V3.2",
	"MiniMax-M3",
	"MiniMax-M2.7",
	"MiniMax-M2.5",
	"MiMo-V2.5-Pro",
}

var scnetVisionModels = []string{"Kimi-K2.6", "Kimi-K2.5"}

func scnetOpenAIModelOverrides() map[string]ProviderModelOverride {
	return map[string]ProviderModelOverride{
		"DeepSeek-V4-Flash": {
			// SCNet's OpenAI-compatible endpoint uses reasoning_effort and leaves
			// enable_thinking on by default; it does not use DeepSeek's native
			// thinking.type request shape.
			ReasoningProtocol: ReasoningProtocolOpenAI,
			SupportedEfforts:  []string{"high", "max"},
			DefaultEffort:     "high",
		},
	}
}

// SCNet (国家超算互联网) exposes a broader catalog through its OpenAI-compatible
// endpoint. Keep this curated list aligned with the Token Plan documentation;
// the UI preserves curated models when the dynamic catalog is also available.
var scnetPreset = ProviderPreset{
	ID:          "scnet",
	Label:       "SCNet",
	Description: "SCNet (国家超算互联网) OpenAI-compatible token-plan API.",
	KeyEnv:      "SCNET_API_KEY",
	Entries: []ProviderEntry{{
		Name:           "scnet",
		Kind:           "openai",
		BaseURL:        "https://api.scnet.cn/api/llm/v1",
		ModelsURL:      "https://api.scnet.cn/api/llm/v1/models",
		Models:         scnetOpenAIModels,
		VisionModels:   scnetVisionModels,
		Default:        "MiniMax-M2.5",
		APIKeyEnv:      "SCNET_API_KEY",
		ModelOverrides: scnetOpenAIModelOverrides(),
	}},
}

// SCNet documents the same current Token Plan catalog for its OpenAI- and
// Anthropic-compatible entrypoints. Keep an explicit curated list here because
// the OpenAI /models endpoint is not an Anthropic model-discovery contract.
var scnetAnthropicModels = append([]string(nil), scnetOpenAIModels...)

var scnetAnthropicPreset = ProviderPreset{
	ID:          "scnet-anthropic",
	Label:       "SCNet Anthropic",
	Description: "SCNet (国家超算互联网) Anthropic-compatible token-plan endpoint with Bearer auth.",
	KeyEnv:      "SCNET_API_KEY",
	Entries: []ProviderEntry{{
		Name:         "scnet-anthropic",
		Kind:         "anthropic",
		BaseURL:      "https://api.scnet.cn/api/llm/anthropic",
		Models:       scnetAnthropicModels,
		VisionModels: scnetVisionModels,
		Default:      "MiniMax-M2.5",
		APIKeyEnv:    "SCNET_API_KEY",
		AuthHeader:   true,
	}},
}

func init() {
	curatedProviderPresets = append(curatedProviderPresets, scnetPreset, scnetAnthropicPreset)
}
