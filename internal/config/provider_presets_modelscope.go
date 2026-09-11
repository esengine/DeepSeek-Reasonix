package config

var modelscopeModels = []string{
	"Qwen/Qwen3.5-397B-A17B",
	"Qwen/Qwen3.5-122B-A10B",
	"Qwen/Qwen3.5-27B",
	"deepseek-ai/DeepSeek-V4-Flash-0731",
	"deepseek-ai/DeepSeek-V4-Pro",
	"MiniMax/MiniMax-M3",
	"ZhipuAI/GLM-5.2",
}

var modelscopePreset = ProviderPreset{
	ID:          "modelscope",
	Label:       "ModelScope",
	Description: "ModelScope community OpenAI-compatible endpoint with Qwen, DeepSeek and other open-source models.",
	KeyEnv:      "MODELSCOPE_API_KEY",
	Entries: []ProviderEntry{{
		Name:      "modelscope",
		Kind:      "openai",
		BaseURL:   "https://api-inference.modelscope.cn/v1",
		Models:    modelscopeModels,
		Default:   "Qwen/Qwen3.5-397B-A17B",
		APIKeyEnv: "MODELSCOPE_API_KEY",
		ModelOverrides: map[string]ProviderModelOverride{
			"Qwen/Qwen3.5-397B-A17B": {Vision: boolPointer(true)},
			"Qwen/Qwen3.5-122B-A10B": {Vision: boolPointer(true)},
			"Qwen/Qwen3.5-27B":       {Vision: boolPointer(true)},
		},
	}},
}
