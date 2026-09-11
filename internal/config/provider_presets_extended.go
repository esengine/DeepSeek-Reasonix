package config

// These presets are independently maintained from providers' public API docs.
// See docs/PROVIDER_CATALOG.md for sources. Catalogs are editable starting points,
// not a claim that every account has access to every listed model.
var extendedProviderPresets = []ProviderPreset{
	newCatalogPreset("doubao-chat", "火山引擎 / 豆包", "DOUBAO_API_KEY", "openai", "https://ark.cn-beijing.volces.com/api/v3", "doubao-seed-2-1-pro-260628"),
	newCatalogPreset("doubao-responses", "火山引擎 / 豆包", "DOUBAO_API_KEY", "responses", "https://ark.cn-beijing.volces.com/api/v3", "doubao-seed-2-1-pro-260628"),
	newCatalogPreset("baidu-cloud", "百度智能云 / 千帆", "BAIDU_API_KEY", "openai", "https://qianfan.baidubce.com/v2", "ernie-4.5-turbo-128k"),
	newCatalogPreset("ppio", "PPIO / 派欧云", "PPIO_API_KEY", "openai", "https://api.ppio.com/openai", "deepseek/deepseek-v3.2"),
	newCatalogPreset("qiniu", "七牛云", "QINIU_API_KEY", "openai", "https://api.qnaigc.com/v1", "deepseek-v3"),
	newCatalogPreset("xai-chat", "xAI / Grok", "XAI_API_KEY", "openai", "https://api.x.ai/v1", "grok-4.3"),
	newCatalogPreset("xai-responses", "xAI / Grok", "XAI_API_KEY", "responses", "https://api.x.ai/v1", "grok-4.3"),
	newCatalogPreset("cerebras", "Cerebras", "CEREBRAS_API_KEY", "openai", "https://api.cerebras.ai/v1", "gpt-oss-120b"),
	newCatalogPreset("together", "Together AI", "TOGETHER_API_KEY", "openai", "https://api.together.ai/v1", "moonshotai/Kimi-K2.6"),
	newCatalogPreset("fireworks-chat", "Fireworks AI", "FIREWORKS_API_KEY", "openai", "https://api.fireworks.ai/inference/v1", "accounts/fireworks/models/deepseek-v3p1"),
	newCatalogPreset("fireworks-anthropic", "Fireworks AI", "FIREWORKS_API_KEY", "anthropic", "https://api.fireworks.ai/inference", "accounts/fireworks/models/deepseek-v3p1"),
	newCatalogPreset("fireworks-responses", "Fireworks AI", "FIREWORKS_API_KEY", "responses", "https://api.fireworks.ai/inference/v1", "accounts/fireworks/models/qwen3-235b-a22b"),

	newCatalogPreset("amd-gpu-cloud", "AMD GPU Cloud", "AMD_GPU_CLOUD_API_KEY", "openai", "https://developer.amd.com.cn/radeon/api/v1", "DeepSeek-V4-Flash", "DeepSeek-V4-Pro", "Qwen3.6-35B-A3B", "GLM-5.1", "GLM-5.2", "gpt-oss-120b", "Kimi-K2.6"),
	{ID: "deepseek-chat", Label: "DeepSeek Chat Completions", KeyEnv: "DEEPSEEK_API_KEY",
		Description: "Official DeepSeek Chat Completions API.",
		Entries:     []ProviderEntry{{Name: "deepseek-chat", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY", Models: deepSeekOfficialModels, Default: "deepseek-v4-flash", ContextWindow: 1_000_000, Prices: deepSeekV4PricesUSD(), ModelOverrides: deepSeekV4EffortOverrides(), WebSearch: boolPointer(false)}},
	},
	newCatalogPreset("openai-responses", "OpenAI", "OPENAI_API_KEY", "responses", "https://api.openai.com/v1", "gpt-4.1"),
	newCatalogPreset("openai-chat", "OpenAI", "OPENAI_API_KEY", "openai", "https://api.openai.com/v1", "gpt-4.1"),
	newCatalogPreset("anthropic", "Anthropic", "ANTHROPIC_API_KEY", "anthropic", "https://api.anthropic.com", "claude-sonnet-4-5"),
	newCatalogPreset("gemini", "Google Gemini", "GEMINI_API_KEY", "openai", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-flash"),
	newCatalogPreset("siliconflow", "SiliconFlow", "SILICONFLOW_API_KEY", "openai", "https://api.siliconflow.cn/v1", "deepseek-ai/DeepSeek-V3.2"),
	newCatalogPreset("openrouter", "OpenRouter", "OPENROUTER_API_KEY", "openai", "https://openrouter.ai/api/v1", "openai/gpt-4.1"),
	newCatalogPreset("groq", "Groq", "GROQ_API_KEY", "openai", "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile"),
	newCatalogPreset("mistral", "Mistral AI", "MISTRAL_API_KEY", "openai", "https://api.mistral.ai/v1", "mistral-small-latest"),
	newCatalogPreset("ollama-local", "Ollama", "OLLAMA_LOCAL_API_KEY", "openai", "http://localhost:11434/v1", "qwen3:8b"),
	newCatalogPreset("lmstudio", "LM Studio", "LM_STUDIO_API_KEY", "openai", "http://localhost:1234/v1", "qwen3-8b"),
}

func newCatalogPreset(id, label, keyEnv, kind, baseURL string, models ...string) ProviderPreset {
	return ProviderPreset{ID: id, Label: label, KeyEnv: keyEnv,
		Description: label + " API. Fetch or edit models for your account after connecting.",
		Entries: []ProviderEntry{{Name: id, Kind: kind, BaseURL: baseURL, APIKeyEnv: keyEnv,
			Models: models, Default: models[0], WebSearch: boolPointer(false)}},
	}
}

func init() {
	curatedProviderPresets = append(curatedProviderPresets, extendedProviderPresets...)
}
