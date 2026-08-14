package config

var scnetModels = []string{"MiniMax-M2.5", "qwen3.7-plus", "qwen3.5-plus", "DeepSeek-R1-Distill-Qwen-7B", "QwQ-32B"}

// SCNet (国家超算互联网) publishes its model catalog dynamically, so both presets
// auto-fetch it from the shared OpenAI-compatible /models endpoint; the static
// list only seeds the picker and stays as the offline fallback.
var scnetPreset = ProviderPreset{
	ID:          "scnet",
	Label:       "SCNet",
	Description: "SCNet (国家超算互联网) OpenAI-compatible token-plan API.",
	KeyEnv:      "SCNET_API_KEY",
	Entries: []ProviderEntry{{
		Name:      "scnet",
		Kind:      "openai",
		BaseURL:   "https://api.scnet.cn/api/llm/v1",
		ModelsURL: "https://api.scnet.cn/api/llm/v1/models",
		Models:    scnetModels,
		Default:   "MiniMax-M2.5",
		APIKeyEnv: "SCNET_API_KEY",
	}},
}

var scnetAnthropicPreset = ProviderPreset{
	ID:          "scnet-anthropic",
	Label:       "SCNet Anthropic",
	Description: "SCNet (国家超算互联网) Anthropic-compatible token-plan endpoint with Bearer auth.",
	KeyEnv:      "SCNET_API_KEY",
	Entries: []ProviderEntry{{
		Name:       "scnet-anthropic",
		Kind:       "anthropic",
		BaseURL:    "https://api.scnet.cn/api/llm/anthropic",
		ModelsURL:  "https://api.scnet.cn/api/llm/v1/models",
		Models:     scnetModels,
		Default:    "MiniMax-M2.5",
		APIKeyEnv:  "SCNET_API_KEY",
		AuthHeader: true,
		Thinking:   "adaptive",
	}},
}

func init() {
	curatedProviderPresets = append(curatedProviderPresets, scnetPreset, scnetAnthropicPreset)
}
