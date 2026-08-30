package config

import (
	"sort"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

// ProviderPreset is a curated, editable provider starter template. Presets are
// not secret-bearing: API key values still live only in Reasonix home .env.
type ProviderPreset struct {
	ID          string
	Label       string
	Description string
	KeyEnv      string
	// Recommended marks the lowest-friction starter path in Settings. It is
	// presentation metadata only; installed entries remain editable providers.
	Recommended bool
	// BillingMode tells the host whether usage should be presented as a
	// subscription-equivalent plan rather than a token price estimate.
	BillingMode string
	Entries     []ProviderEntry

	// Display metadata is presentation-only. It deliberately does not change
	// provider routing or the persisted provider schema.
	DisplayGroup   string
	DisplaySection string
	DisplayTier    string
	RouteKind      string
	Optional       bool
	DisplayOrder   int
}

const (
	ProviderPresetVersion        = 1
	longCat20ContextWindow       = 1_048_576
	legacyLongCat20ContextWindow = 131_072
	longCatOpenAIBaseURL         = "https://api.longcat.chat/openai/v1"
	longCatAnthropicBaseURL      = "https://api.longcat.chat/anthropic"
	deepSeekAnthropicBaseURL     = "https://api.deepseek.com/anthropic"
)

// CuratedProviderPresets returns one-click provider templates for common
// OpenAI-compatible and Anthropic-compatible coding-plan services. These are
// intentionally editable after installation; they reduce setup friction without
// turning fast-moving third-party catalogs into hard runtime dependencies.
func CuratedProviderPresets() []ProviderPreset {
	presets := make([]ProviderPreset, 0, len(curatedProviderPresets))
	for _, preset := range curatedProviderPresets {
		// Keep the old direct lookup available for installed configurations, but
		// do not offer the redundant Anthropic preset in new-provider surfaces.
		if preset.ID == "deepseek-anthropic" {
			continue
		}
		presets = append(presets, cloneProviderPreset(preset))
	}
	sort.SliceStable(presets, func(i, j int) bool {
		return providerPresetDisplayRank(presets[i].ID) < providerPresetDisplayRank(presets[j].ID)
	})
	return presets
}

// CuratedProviderPreset returns a single provider preset by id.
func CuratedProviderPreset(id string) (ProviderPreset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range curatedProviderPresets {
		if p.ID == id {
			return cloneProviderPreset(p), true
		}
	}
	return ProviderPreset{}, false
}

func providerPresetDisplayRank(id string) int {
	switch {
	case id == "opencode-go-recommended":
		return -3
	case id == "deepseek-responses":
		return -2
	case id == "glm-cn" || id == "zai-global" || strings.HasPrefix(id, "glm-coding-plan-") || strings.HasPrefix(id, "zai-coding-plan-"):
		return 0
	case strings.HasPrefix(id, "longcat-"):
		return 1
	case id == "token-rhythm":
		return 1
	case strings.HasPrefix(id, "kimi-"):
		return 2
	case strings.HasPrefix(id, "minimax-"):
		return 3
	default:
		return 4
	}
}

var (
	legacyKimiAPIModels = []string{"kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-k2.5"}
	kimiAPIModels       = []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-k2.5"}
	kimiAPIVisionModels = []string{"kimi-k3", "kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-k2.5"}
	kimiCodingModels    = []string{"kimi-for-coding"}

	longCat20Models              = []string{"LongCat-2.0"}
	deepSeekOfficialModels       = []string{"deepseek-v4-flash", "deepseek-v4-pro", openai.OfficialDeepSeekVisionModel}
	deepSeekOfficialVisionModels = []string{openai.OfficialDeepSeekVisionModel}
	tokenRhythmModels            = []string{
		"deepseek-v4-flash", "deepseek-v4-pro", "glm-5", "glm-5.1",
		"minimax-m2.7", "kimi-k2.5", "kimi-k2.6", "minimax-m2.5",
		"mimo-v2.5-pro", "qwen3.7-max", "kimi-k2.7-code", "glm-5.2",
		"qwen3.8-max", "deepseek-v4-flash-0731",
	}
	tokenRhythmVisionModels = []string{"kimi-k2.5", "kimi-k2.6", "kimi-k2.7-code"}

	mimoV25Models       = []string{"mimo-v2.5-pro", "mimo-v2.5"}
	mimoV25VisionModels = []string{"mimo-v2.5"}

	minimaxMSeriesModels       = []string{"MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"}
	minimaxMSeriesVisionModels = []string{"MiniMax-M3"}

	glmAPIModels       = []string{"glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo", "glm-5v-turbo", "glm-4.7", "glm-4.7-flash", "glm-4.7-flashx", "glm-4.6", "glm-4.5", "glm-4.5-air", "glm-4.5-flash"}
	glmAPIVisionModels = []string{"glm-5v-turbo"}
	glmCodingModels    = []string{"glm-5.2", "glm-5.1", "glm-5", "glm-4.7"}
	glmAnthropicModels = []string{"glm-5.2[1m]", "glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.5-air"}

	qwenAPIModels        = []string{"qwen3.7-plus", "qwen3.7-max", "qwen3.6-plus", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "MiniMax-M2.5", "glm-5", "glm-4.7", "kimi-k2.5"}
	qwenAPIVisionModels  = []string{"qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"}
	qwenPlanModels       = []string{"qwen3.7-plus", "qwen3.6-plus", "kimi-k2.5", "glm-5", "MiniMax-M2.5", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "glm-4.7"}
	qwenPlanVisionModels = []string{"qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"}

	stepfunPlanModels = []string{"step-3.7-flash", "step-3.5-flash", "step-3.5-flash-2603"}

	// Only step-3.7-flash is enabled server-side on the Responses API
	// ("this model is not enabled for the Responses API" for 3.5 SKUs).
	stepfunResponsesModels = []string{"step-3.7-flash"}

	// The pay-as-you-go channel serves the same reasoning SKUs; 3.7-flash
	// additionally accepts image input there (verified live), while the
	// step_plan channel rejects images outright.
	stepfunAPIModels       = []string{"step-3.7-flash", "step-3.5-flash", "step-3.5-flash-2603"}
	stepfunAPIVisionModels = []string{"step-3.7-flash"}

	legacyOpenCodeGoModels           = []string{"glm-5.2", "glm-5.1", "kimi-k2.7-code", "kimi-k2.6", "deepseek-v4-pro", "deepseek-v4-flash", "mimo-v2.5-pro", "mimo-v2.5"}
	opencodeGoModels                 = []string{"glm-5.3", "glm-5.2", "glm-5.1", "kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "deepseek-v4-pro", "deepseek-v4-flash", "mimo-v2.5-pro", "mimo-v2.5", "hy3"}
	opencodeGoVisionModels           = []string{"kimi-k3"}
	opencodeZenAnthropicModels       = []string{"claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5", "qwen3.6-plus", "qwen3.5-plus", "qwen3.6-plus-free"}
	opencodeZenAnthropicVisionModels = []string{"claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5"}

	novitaModels      = []string{"zai-org/glm-5.2", "moonshotai/kimi-k2.7-code", "minimax/minimax-m3", "deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash", "qwen/qwen3.7-max", "qwen/qwen3.6-plus", "zai-org/glm-5v-turbo"}
	gmiModels         = []string{"zai-org/GLM-5.2-FP8", "deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4-Flash", "moonshotai/Kimi-K2.7-Code", "anthropic/claude-sonnet-4.6", "openai/gpt-5.5"}
	vercelModels      = []string{"anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8", "openai/gpt-5.4", "openai/gpt-5.4-pro", "moonshotai/kimi-k2.7-code", "zai/glm-5.2", "deepseek/deepseek-v4-pro"}
	huggingFaceModels = []string{"zai-org/GLM-5.2", "deepseek-ai/DeepSeek-V3.2", "Qwen/Qwen3.5-72B-Instruct"}
	nvidiaModels      = []string{"nvidia/nemotron-3-nano-30b-a3b", "nvidia/nemotron-3-super-120b-a12b", "nvidia/nemotron-3-ultra-550b-a55b", "deepseek-ai/deepseek-v4-pro", "qwen/qwen3.5-397b-a17b"}
	ollamaCloudModels = []string{"glm-5.2", "kimi-k2.7-code", "deepseek-v4-pro", "deepseek-v4-flash", "minimax-m3", "nemotron-3-nano:30b", "qwen3-coder-next"}
)

func qwenModelContextOverrides() map[string]ProviderModelOverride {
	return map[string]ProviderModelOverride{
		"qwen3-max-2026-01-23": {ContextWindow: 262_144},
		"qwen3-coder-next":     {ContextWindow: 262_144},
		"MiniMax-M2.5":         {ContextWindow: 196_608},
		"glm-5":                {ContextWindow: 202_752},
		"glm-4.7":              {ContextWindow: 202_752},
		"kimi-k2.5":            {ContextWindow: 262_144},
	}
}

func tokenRhythmModelOverrides() map[string]ProviderModelOverride {
	return map[string]ProviderModelOverride{
		"deepseek-v4-flash": {
			ReasoningProtocol: ReasoningProtocolDeepSeek,
			SupportedEfforts:  []string{"disabled", "low", "high", "max"},
			DefaultEffort:     "high",
		},
		"deepseek-v4-pro": {
			ReasoningProtocol: ReasoningProtocolDeepSeek,
			SupportedEfforts:  []string{"disabled", "high", "max"},
			DefaultEffort:     "high",
		},
		"deepseek-v4-flash-0731": {
			ReasoningProtocol: ReasoningProtocolDeepSeek,
			SupportedEfforts:  []string{"disabled", "low", "high", "max"},
			DefaultEffort:     "high",
		},
		"glm-5": {
			ReasoningProtocol: ReasoningProtocolGLM,
			SupportedEfforts:  []string{"enabled", "disabled"},
			DefaultEffort:     "enabled",
		},
		"glm-5.1": {
			ReasoningProtocol: ReasoningProtocolGLM,
			SupportedEfforts:  []string{"enabled", "disabled"},
			DefaultEffort:     "enabled",
			ContextWindow:     200_000,
		},
		"glm-5.2": {
			ReasoningProtocol: ReasoningProtocolGLM,
			SupportedEfforts:  []string{"enabled", "disabled"},
			DefaultEffort:     "enabled",
		},
		"minimax-m2.7":   {ContextWindow: 200_000},
		"kimi-k2.5":      {ContextWindow: 256_000},
		"kimi-k2.6":      {ContextWindow: 256_000},
		"minimax-m2.5":   {ContextWindow: 200_000},
		"mimo-v2.5-pro":  {ContextWindow: 256_000},
		"kimi-k2.7-code": {ContextWindow: 256_000},
	}
}

func kimiK3DirectOverride() ProviderModelOverride {
	return ProviderModelOverride{
		ReasoningProtocol: ReasoningProtocolOpenAI,
		SupportedEfforts:  []string{"low", "high", "max"},
		DefaultEffort:     "max",
		ContextWindow:     1_048_576,
	}
}

var curatedProviderPresets = []ProviderPreset{
	{
		ID:          "deepseek-anthropic",
		Label:       "DeepSeek Official Anthropic",
		Description: "Separate official DeepSeek Anthropic-compatible entry for Flash and Pro.",
		KeyEnv:      "DEEPSEEK_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "deepseek-anthropic",
			Kind:           "anthropic",
			BaseURL:        deepSeekAnthropicBaseURL,
			Models:         deepSeekOfficialModels,
			Default:        "deepseek-v4-flash",
			VisionModels:   deepSeekOfficialVisionModels,
			APIKeyEnv:      "DEEPSEEK_API_KEY",
			BalanceURL:     "https://api.deepseek.com/user/balance",
			Thinking:       "enabled",
			WebSearch:      boolPointer(true),
			ContextWindow:  1_000_000,
			Prices:         deepSeekV4PricesUSD(),
			ModelOverrides: deepSeekV4EffortOverrides(),
		}},
	},
	{
		ID:          "longcat-openai",
		Label:       "LongCat OpenAI",
		Description: "LongCat Platform OpenAI-compatible endpoint for LongCat-2.0.",
		KeyEnv:      "LONGCAT_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "longcat-openai",
			Kind:             "openai",
			BaseURL:          longCatOpenAIBaseURL,
			ModelsURL:        "https://api.longcat.chat/openai/v1/models",
			Models:           longCat20Models,
			Default:          "LongCat-2.0",
			APIKeyEnv:        "LONGCAT_API_KEY",
			ContextWindow:    longCat20ContextWindow,
			Prices:           longCat20Prices(longCat20Models),
			Thinking:         "enabled",
			SupportedEfforts: []string{"enabled", "disabled"},
			DefaultEffort:    "enabled",
		}},
	},
	{
		ID:          "longcat-anthropic",
		Label:       "LongCat Anthropic",
		Description: "LongCat Platform Anthropic-compatible Messages endpoint for LongCat-2.0.",
		KeyEnv:      "LONGCAT_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "longcat-anthropic",
			Kind:             "anthropic",
			BaseURL:          longCatAnthropicBaseURL,
			ModelsURL:        "https://api.longcat.chat/anthropic/v1/models",
			Models:           longCat20Models,
			Default:          "LongCat-2.0",
			APIKeyEnv:        "LONGCAT_API_KEY",
			AuthHeader:       true,
			Thinking:         "enabled",
			SupportedEfforts: []string{"enabled", "disabled"},
			DefaultEffort:    "enabled",
			ContextWindow:    longCat20ContextWindow,
			Prices:           longCat20Prices(longCat20Models),
		}},
	},
	{
		ID:          "token-rhythm",
		Label:       "Token Rhythm",
		Description: "Token Rhythm (基元律动) multi-model OpenAI-compatible gateway.",
		KeyEnv:      "TOKEN_RHYTHM_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "token-rhythm",
			Kind:           "openai",
			BaseURL:        "https://tokenrhythm.studio/v1",
			ModelsURL:      "https://tokenrhythm.studio/v1/models",
			Models:         tokenRhythmModels,
			VisionModels:   tokenRhythmVisionModels,
			Default:        "deepseek-v4-flash",
			APIKeyEnv:      "TOKEN_RHYTHM_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: tokenRhythmModelOverrides(),
		}},
	},
	{
		ID:          "kimi-cn",
		Label:       "Kimi CN API",
		Description: "Moonshot Kimi China OpenAI-compatible API.",
		KeyEnv:      "KIMI_API_KEY",
		Entries: []ProviderEntry{{
			Name:              "kimi-cn",
			Kind:              "openai",
			BaseURL:           "https://api.moonshot.cn/v1",
			Models:            kimiAPIModels,
			VisionModels:      kimiAPIVisionModels,
			Default:           "kimi-k2.7-code",
			APIKeyEnv:         "KIMI_API_KEY",
			BalanceURL:        "https://api.moonshot.cn/v1/users/me/balance",
			ContextWindow:     262144,
			ReasoningProtocol: ReasoningProtocolNone,
			ModelOverrides: map[string]ProviderModelOverride{
				"kimi-k3": kimiK3DirectOverride(),
			},
		}},
	},
	{
		ID:          "kimi-global",
		Label:       "Kimi Global API",
		Description: "Moonshot Kimi international OpenAI-compatible API.",
		KeyEnv:      "MOONSHOT_API_KEY",
		Entries: []ProviderEntry{{
			Name:              "kimi-global",
			Kind:              "openai",
			BaseURL:           "https://api.moonshot.ai/v1",
			Models:            kimiAPIModels,
			VisionModels:      kimiAPIVisionModels,
			Default:           "kimi-k2.7-code",
			APIKeyEnv:         "MOONSHOT_API_KEY",
			BalanceURL:        "https://api.moonshot.ai/v1/users/me/balance",
			ContextWindow:     262144,
			ReasoningProtocol: ReasoningProtocolNone,
			ModelOverrides: map[string]ProviderModelOverride{
				"kimi-k3": kimiK3DirectOverride(),
			},
		}},
	},
	{
		ID:          "kimi-coding-plan",
		Label:       "Kimi Coding Plan",
		Description: "Kimi Coding Plan via its dedicated Anthropic-compatible endpoint.",
		KeyEnv:      "KIMI_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "kimi-coding-plan",
			Kind:          "anthropic",
			BaseURL:       "https://api.kimi.com/coding/",
			Models:        kimiCodingModels,
			VisionModels:  kimiCodingModels,
			Default:       "kimi-for-coding",
			APIKeyEnv:     "KIMI_CODING_API_KEY",
			Headers:       map[string]string{"User-Agent": "claude-code/0.1.0"},
			Thinking:      "adaptive",
			ContextWindow: 262144,
		}},
	},
	{
		ID:          "mimo-api",
		Label:       "MiMo API",
		Description: "Xiaomi MiMo direct API with text and vision-capable models.",
		KeyEnv:      "MIMO_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-api",
			Kind:          "openai",
			BaseURL:       "https://api.xiaomimimo.com/v1",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
			NoProxy:       true,
		}},
	},
	{
		ID:          "mimo-anthropic",
		Label:       "MiMo Anthropic",
		Description: "Xiaomi MiMo direct Anthropic-compatible endpoint.",
		KeyEnv:      "MIMO_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://api.xiaomimimo.com/anthropic",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
			NoProxy:       true,
		}},
	},
	{
		ID:          "mimo-token-plan-cn",
		Label:       "MiMo Token Plan CN",
		Description: "Xiaomi MiMo token-plan China endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-cn",
			Kind:          "openai",
			BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
			NoProxy:       true,
		}},
	},
	{
		ID:          "mimo-token-plan-cn-anthropic",
		Label:       "MiMo Token Plan CN Anthropic",
		Description: "Xiaomi MiMo token-plan China Anthropic-compatible endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-cn-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://token-plan-cn.xiaomimimo.com/anthropic",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
			NoProxy:       true,
		}},
	},
	{
		ID:          "mimo-token-plan-sgp",
		Label:       "MiMo Token Plan SGP",
		Description: "Xiaomi MiMo token-plan Singapore endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-sgp",
			Kind:          "openai",
			BaseURL:       "https://token-plan-sgp.xiaomimimo.com/v1",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
		}},
	},
	{
		ID:          "mimo-token-plan-sgp-anthropic",
		Label:       "MiMo Token Plan SGP Anthropic",
		Description: "Xiaomi MiMo token-plan Singapore Anthropic-compatible endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-sgp-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://token-plan-sgp.xiaomimimo.com/anthropic",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
		}},
	},
	{
		ID:          "mimo-token-plan-ams",
		Label:       "MiMo Token Plan AMS",
		Description: "Xiaomi MiMo token-plan Amsterdam endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-ams",
			Kind:          "openai",
			BaseURL:       "https://token-plan-ams.xiaomimimo.com/v1",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
		}},
	},
	{
		ID:          "mimo-token-plan-ams-anthropic",
		Label:       "MiMo Token Plan AMS Anthropic",
		Description: "Xiaomi MiMo token-plan Amsterdam Anthropic-compatible endpoint.",
		KeyEnv:      "MIMO_TOKEN_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-token-plan-ams-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://token-plan-ams.xiaomimimo.com/anthropic",
			Models:        mimoV25Models,
			VisionModels:  mimoV25VisionModels,
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: 1048576,
			Prices:        mimoDomesticPrices(mimoV25Models),
		}},
	},
	{
		ID:          "minimax-cn-api",
		Label:       "MiniMax CN API",
		Description: "MiniMax China OpenAI-compatible M-series API endpoint.",
		KeyEnv:      "MINIMAX_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "minimax-cn-api",
			Kind:          "openai",
			BaseURL:       "https://api.minimaxi.com/v1",
			Models:        minimaxMSeriesModels,
			VisionModels:  minimaxMSeriesVisionModels,
			Default:       "MiniMax-M3",
			APIKeyEnv:     "MINIMAX_API_KEY",
			ContextWindow: 1048576,
			ExtraBody:     map[string]any{"reasoning_split": true},
		}},
	},
	{
		ID:          "minimax-global-api",
		Label:       "MiniMax Global API",
		Description: "MiniMax international OpenAI-compatible M-series API endpoint.",
		KeyEnv:      "MINIMAX_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "minimax-global-api",
			Kind:          "openai",
			BaseURL:       "https://api.minimax.io/v1",
			Models:        minimaxMSeriesModels,
			VisionModels:  minimaxMSeriesVisionModels,
			Default:       "MiniMax-M3",
			APIKeyEnv:     "MINIMAX_API_KEY",
			ContextWindow: 1048576,
			ExtraBody:     map[string]any{"reasoning_split": true},
		}},
	},
	{
		ID:          "minimax-cn-anthropic",
		Label:       "MiniMax CN Anthropic",
		Description: "MiniMax China Anthropic-compatible M-series endpoint.",
		KeyEnv:      "MINIMAX_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "minimax-cn-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://api.minimaxi.com/anthropic",
			Models:        minimaxMSeriesModels,
			VisionModels:  minimaxMSeriesVisionModels,
			Default:       "MiniMax-M3",
			APIKeyEnv:     "MINIMAX_PLAN_API_KEY",
			AuthHeader:    true,
			ContextWindow: 1048576,
		}},
	},
	{
		ID:          "minimax-global-anthropic",
		Label:       "MiniMax Global Anthropic",
		Description: "MiniMax international Anthropic-compatible endpoint with Bearer auth.",
		KeyEnv:      "MINIMAX_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "minimax-global-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://api.minimax.io/anthropic",
			Models:        minimaxMSeriesModels,
			VisionModels:  minimaxMSeriesVisionModels,
			Default:       "MiniMax-M3",
			APIKeyEnv:     "MINIMAX_API_KEY",
			AuthHeader:    true,
			ContextWindow: 1048576,
		}},
	},
	{
		ID:          "deepseek-responses",
		Label:       "DeepSeek Official Responses API",
		Description: "Official stateless DeepSeek Responses API for Flash and Pro with server-side web search.",
		KeyEnv:      "DEEPSEEK_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "deepseek-responses",
			Kind:           "responses",
			BaseURL:        "https://api.deepseek.com",
			Models:         deepSeekOfficialModels,
			Default:        "deepseek-v4-flash",
			VisionModels:   deepSeekOfficialVisionModels,
			APIKeyEnv:      "DEEPSEEK_API_KEY",
			BalanceURL:     "https://api.deepseek.com/user/balance",
			ContextWindow:  1_000_000,
			Prices:         deepSeekV4PricesUSD(),
			ResponsesMode:  "stateless",
			WebSearch:      boolPointer(true),
			ModelOverrides: deepSeekV4EffortOverrides(),
		}},
	},
	{
		ID:          "glm-cn",
		Label:       "GLM CN API",
		Description: "Zhipu GLM China OpenAI-compatible API with thinking controls.",
		KeyEnv:      "GLM_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "glm-cn",
			Kind:          "openai",
			BaseURL:       "https://open.bigmodel.cn/api/paas/v4",
			Models:        glmAPIModels,
			VisionModels:  glmAPIVisionModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "GLM_API_KEY",
			ContextWindow: 1000000,
		}},
	},
	{
		ID:          "zai-global",
		Label:       "Z.AI Global API",
		Description: "Z.AI international OpenAI-compatible GLM API.",
		KeyEnv:      "ZAI_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "zai-global",
			Kind:          "openai",
			BaseURL:       "https://api.z.ai/api/paas/v4",
			Models:        glmAPIModels,
			VisionModels:  glmAPIVisionModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "ZAI_API_KEY",
			ContextWindow: 1000000,
		}},
	},
	{
		ID:          "glm-coding-plan-cn",
		Label:       "GLM Coding Plan CN",
		Description: "Zhipu GLM China coding-plan endpoint.",
		KeyEnv:      "GLM_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "glm-coding-plan-cn",
			Kind:          "openai",
			BaseURL:       "https://open.bigmodel.cn/api/coding/paas/v4",
			Models:        glmCodingModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "GLM_PLAN_API_KEY",
			ContextWindow: 1000000,
			NoProxy:       true,
		}},
	},
	{
		ID:          "glm-coding-plan-cn-anthropic",
		Label:       "GLM Coding Plan CN Anthropic",
		Description: "Zhipu GLM China coding-plan Anthropic-compatible endpoint.",
		KeyEnv:      "GLM_PLAN_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "glm-coding-plan-cn-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://open.bigmodel.cn/api/anthropic",
			Models:        glmAnthropicModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "GLM_PLAN_API_KEY",
			AuthHeader:    true,
			Thinking:      "adaptive",
			ContextWindow: 1000000,
			NoProxy:       true,
		}},
	},
	{
		ID:          "zai-coding-plan-global",
		Label:       "Z.AI Coding Plan Global",
		Description: "Z.AI international coding-plan endpoint.",
		KeyEnv:      "ZAI_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "zai-coding-plan-global",
			Kind:          "openai",
			BaseURL:       "https://api.z.ai/api/coding/paas/v4",
			Models:        glmCodingModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "ZAI_CODING_API_KEY",
			ContextWindow: 1000000,
		}},
	},
	{
		ID:          "zai-coding-plan-global-anthropic",
		Label:       "Z.AI Coding Plan Global Anthropic",
		Description: "Z.AI international coding-plan Anthropic-compatible endpoint.",
		KeyEnv:      "ZAI_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "zai-coding-plan-global-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://api.z.ai/api/anthropic",
			Models:        glmAnthropicModels,
			Default:       "glm-5.2",
			APIKeyEnv:     "ZAI_CODING_API_KEY",
			AuthHeader:    true,
			Thinking:      "adaptive",
			ContextWindow: 1000000,
		}},
	},
	{
		ID:             "opencode-go",
		Label:          "OpenCode Go",
		Description:    "OpenCode Go relay with per-model capability overrides.",
		KeyEnv:         "OPENCODE_GO_API_KEY",
		BillingMode:    "subscription_equivalent",
		DisplayGroup:   "opencode",
		DisplaySection: "go",
		DisplayTier:    "compatibility",
		RouteKind:      "chat",
		DisplayOrder:   90,
		Entries: []ProviderEntry{{
			Name:            "opencode-go",
			Kind:            "openai",
			BaseURL:         "https://opencode.ai/zen/go/v1",
			Models:          opencodeGoModels,
			VisionModels:    opencodeGoVisionModels,
			Default:         "glm-5.2",
			APIKeyEnv:       "OPENCODE_GO_API_KEY",
			ContextWindow:   128000,
			MaxOutputTokens: 32_768,
			BillingMode:     "subscription_equivalent",
			ModelOverrides: withOpenCodeGoChatContextOverrides(map[string]ProviderModelOverride{
				"glm-5.3": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"low", "high", "max"},
					DefaultEffort:     "high",
				},
				"glm-5.2": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"high", "max"},
					DefaultEffort:     "high",
				},
				"deepseek-v4-flash": {
					ReasoningProtocol: ReasoningProtocolDeepSeek,
					SupportedEfforts:  []string{"disabled", "high", "max"},
					DefaultEffort:     "high",
				},
				"deepseek-v4-pro": {
					ReasoningProtocol: ReasoningProtocolDeepSeek,
					SupportedEfforts:  []string{"disabled", "high", "max"},
					DefaultEffort:     "high",
				},
				"kimi-k2.6": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"low", "medium", "high"},
					DefaultEffort:     "high",
				},
				"kimi-k2.7-code": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"low", "medium", "high"},
					DefaultEffort:     "high",
				},
				"kimi-k3": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"high", "max"},
					DefaultEffort:     "max",
				},
				"hy3": {
					ReasoningProtocol: ReasoningProtocolOpenAI,
					SupportedEfforts:  []string{"none", "low", "high"},
					DefaultEffort:     "high",
				},
			}),
		}},
	},
	opencodeGoAnthropicPreset,
	opencodeGoResponsesPreset,
	opencodeGoDeepSeekAnthropicPreset,
	opencodeGoDeepSeekResponsesPreset,
	opencodeGoRecommendedPreset,
	{
		ID:             "opencode-zen-anthropic",
		Label:          "OpenCode Zen Anthropic",
		Description:    "OpenCode Zen Anthropic-compatible route for Claude and Qwen models.",
		KeyEnv:         "OPENCODE_API_KEY",
		DisplayGroup:   "opencode",
		DisplaySection: "zen",
		DisplayTier:    "primary",
		RouteKind:      "zen-anthropic",
		DisplayOrder:   100,
		Entries: []ProviderEntry{{
			Name:          "opencode-zen-anthropic",
			Kind:          "anthropic",
			BaseURL:       "https://opencode.ai/zen",
			Models:        opencodeZenAnthropicModels,
			VisionModels:  opencodeZenAnthropicVisionModels,
			Default:       "claude-sonnet-4-6",
			APIKeyEnv:     "OPENCODE_API_KEY",
			ContextWindow: 262144,
		}},
	},
	{
		ID:          "qwen-cn",
		Label:       "Qwen CN API",
		Description: "Alibaba DashScope China standard OpenAI-compatible endpoint.",
		KeyEnv:      "QWEN_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-cn",
			Kind:           "openai",
			BaseURL:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Models:         qwenAPIModels,
			VisionModels:   qwenAPIVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
			NoProxy:        true,
		}},
	},
	{
		ID:          "qwen-global",
		Label:       "Qwen Global API",
		Description: "Alibaba DashScope international standard OpenAI-compatible endpoint.",
		KeyEnv:      "QWEN_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-global",
			Kind:           "openai",
			BaseURL:        "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			Models:         qwenAPIModels,
			VisionModels:   qwenAPIVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
		}},
	},
	{
		ID:          "qwen-coding-plan-cn",
		Label:       "Qwen Coding Plan CN",
		Description: "Alibaba Cloud Qwen Coding Plan China endpoint.",
		KeyEnv:      "QWEN_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-coding-plan-cn",
			Kind:           "openai",
			BaseURL:        "https://coding.dashscope.aliyuncs.com/v1",
			Models:         qwenPlanModels,
			VisionModels:   qwenPlanVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_CODING_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
			NoProxy:        true,
		}},
	},
	{
		ID:          "qwen-coding-plan-cn-anthropic",
		Label:       "Qwen Coding Plan CN Anthropic",
		Description: "Alibaba Cloud Qwen Coding Plan China Anthropic-compatible endpoint.",
		KeyEnv:      "QWEN_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-coding-plan-cn-anthropic",
			Kind:           "anthropic",
			BaseURL:        "https://coding.dashscope.aliyuncs.com/apps/anthropic",
			Models:         qwenPlanModels,
			VisionModels:   qwenPlanVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_CODING_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
			Thinking:       "adaptive",
			NoProxy:        true,
		}},
	},
	{
		ID:          "qwen-coding-plan-global",
		Label:       "Qwen Coding Plan Global",
		Description: "Alibaba Cloud Qwen Coding Plan international endpoint.",
		KeyEnv:      "QWEN_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-coding-plan-global",
			Kind:           "openai",
			BaseURL:        "https://coding-intl.dashscope.aliyuncs.com/v1",
			Models:         qwenPlanModels,
			VisionModels:   qwenPlanVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_CODING_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
		}},
	},
	{
		ID:          "qwen-coding-plan-global-anthropic",
		Label:       "Qwen Coding Plan Global Anthropic",
		Description: "Alibaba Cloud Qwen Coding Plan international Anthropic-compatible endpoint.",
		KeyEnv:      "QWEN_CODING_API_KEY",
		Entries: []ProviderEntry{{
			Name:           "qwen-coding-plan-global-anthropic",
			Kind:           "anthropic",
			BaseURL:        "https://coding-intl.dashscope.aliyuncs.com/apps/anthropic",
			Models:         qwenPlanModels,
			VisionModels:   qwenPlanVisionModels,
			Default:        "qwen3.7-plus",
			APIKeyEnv:      "QWEN_CODING_API_KEY",
			ContextWindow:  1_000_000,
			ModelOverrides: qwenModelContextOverrides(),
			Thinking:       "adaptive",
		}},
	},
	{
		ID:          "stepfun",
		Label:       "StepFun",
		Description: "StepFun coding-plan OpenAI-compatible endpoint.",
		KeyEnv:      "STEPFUN_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "stepfun",
			Kind:             "openai",
			BaseURL:          "https://api.stepfun.com/step_plan/v1",
			Models:           stepfunPlanModels,
			Default:          "step-3.7-flash",
			APIKeyEnv:        "STEPFUN_API_KEY",
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "stepfun-responses",
		Label:       "StepFun Responses API",
		Description: "StepFun Responses API endpoint with reasoning effort and tool calls (step-3.7-flash).",
		KeyEnv:      "STEPFUN_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "stepfun-responses",
			Kind:             "responses",
			BaseURL:          "https://api.stepfun.com/v1",
			Models:           stepfunResponsesModels,
			Default:          "step-3.7-flash",
			APIKeyEnv:        "STEPFUN_API_KEY",
			ResponsesMode:    "stateless",
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "stepfun-anthropic",
		Label:       "StepFun Anthropic",
		Description: "StepFun coding-plan Anthropic-compatible endpoint.",
		KeyEnv:      "STEPFUN_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "stepfun-anthropic",
			Kind:             "anthropic",
			BaseURL:          "https://api.stepfun.com/step_plan",
			Models:           stepfunPlanModels,
			Default:          "step-3.7-flash",
			APIKeyEnv:        "STEPFUN_API_KEY",
			Thinking:         "adaptive",
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "stepfun-api",
		Label:       "StepFun API Pay-as-you-go",
		Description: "StepFun pay-as-you-go OpenAI-compatible API with vision on step-3.7-flash.",
		KeyEnv:      "STEPFUN_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "stepfun-api",
			Kind:             "openai",
			BaseURL:          "https://api.stepfun.com/v1",
			Models:           stepfunAPIModels,
			VisionModels:     stepfunAPIVisionModels,
			Default:          "step-3.7-flash",
			APIKeyEnv:        "STEPFUN_API_KEY",
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "stepfun-api-anthropic",
		Label:       "StepFun API Anthropic Pay-as-you-go",
		Description: "StepFun pay-as-you-go Anthropic-compatible Messages API with automatic prefix caching.",
		KeyEnv:      "STEPFUN_API_KEY",
		Entries: []ProviderEntry{{
			Name:             "stepfun-api-anthropic",
			Kind:             "anthropic",
			BaseURL:          "https://api.stepfun.com",
			Models:           stepfunAPIModels,
			Default:          "step-3.7-flash",
			APIKeyEnv:        "STEPFUN_API_KEY",
			Thinking:         "adaptive",
			SupportedEfforts: []string{"low", "medium", "high"},
			DefaultEffort:    "medium",
		}},
	},
	{
		ID:          "novita",
		Label:       "NovitaAI",
		Description: "NovitaAI OpenAI-compatible multi-model gateway.",
		KeyEnv:      "NOVITA_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "novita",
			Kind:      "openai",
			BaseURL:   "https://api.novita.ai/openai/v1",
			Models:    novitaModels,
			Default:   "zai-org/glm-5.2",
			APIKeyEnv: "NOVITA_API_KEY",
		}},
	},
	{
		ID:          "gmi",
		Label:       "GMI Cloud",
		Description: "GMI Cloud direct multi-model OpenAI-compatible gateway.",
		KeyEnv:      "GMI_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "gmi",
			Kind:      "openai",
			BaseURL:   "https://api.gmi-serving.com/v1",
			Models:    gmiModels,
			Default:   "zai-org/GLM-5.2-FP8",
			APIKeyEnv: "GMI_API_KEY",
			Headers:   map[string]string{"User-Agent": "Reasonix"},
		}},
	},
	{
		ID:          "vercel-ai-gateway",
		Label:       "Vercel AI Gateway",
		Description: "Vercel AI Gateway via Anthropic-compatible Messages API.",
		KeyEnv:      "AI_GATEWAY_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "vercel-ai-gateway",
			Kind:          "anthropic",
			BaseURL:       "https://ai-gateway.vercel.sh",
			Models:        vercelModels,
			VisionModels:  []string{"anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8", "openai/gpt-5.4", "openai/gpt-5.4-pro", "moonshotai/kimi-k2.7-code"},
			Default:       "anthropic/claude-sonnet-4.6",
			APIKeyEnv:     "AI_GATEWAY_API_KEY",
			AuthHeader:    true,
			ContextWindow: 1000000,
		}},
	},
	{
		ID:          "huggingface",
		Label:       "HuggingFace Router",
		Description: "HuggingFace Inference Router OpenAI-compatible endpoint.",
		KeyEnv:      "HF_TOKEN",
		Entries: []ProviderEntry{{
			Name:      "huggingface",
			Kind:      "openai",
			BaseURL:   "https://router.huggingface.co/v1",
			Models:    huggingFaceModels,
			Default:   "zai-org/GLM-5.2",
			APIKeyEnv: "HF_TOKEN",
		}},
	},
	{
		ID:          "nvidia",
		Label:       "NVIDIA NIM",
		Description: "NVIDIA NIM OpenAI-compatible accelerated inference endpoint.",
		KeyEnv:      "NVIDIA_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "nvidia",
			Kind:      "openai",
			BaseURL:   "https://integrate.api.nvidia.com/v1",
			Models:    nvidiaModels,
			Default:   "nvidia/nemotron-3-nano-30b-a3b",
			APIKeyEnv: "NVIDIA_API_KEY",
		}},
	},
	{
		ID:          "kilocode",
		Label:       "KiloCode",
		Description: "Kilo Code gateway OpenAI-compatible endpoint.",
		KeyEnv:      "KILOCODE_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "kilocode",
			Kind:      "openai",
			BaseURL:   "https://api.kilo.ai/api/gateway",
			Models:    []string{"kilo/auto"},
			Default:   "kilo/auto",
			APIKeyEnv: "KILOCODE_API_KEY",
		}},
	},
	{
		ID:          "ollama-cloud",
		Label:       "Ollama Cloud",
		Description: "Hosted Ollama Cloud OpenAI-compatible endpoint with max reasoning effort.",
		KeyEnv:      "OLLAMA_API_KEY",
		Entries: []ProviderEntry{{
			Name:      "ollama-cloud",
			Kind:      "openai",
			BaseURL:   "https://ollama.com/v1",
			Models:    ollamaCloudModels,
			Default:   "glm-5.2",
			APIKeyEnv: "OLLAMA_API_KEY",
		}},
	},
}

func boolPointer(value bool) *bool {
	return &value
}

func cloneProviderPreset(p ProviderPreset) ProviderPreset {
	p.Entries = cloneProviderEntries(p.Entries)
	for i := range p.Entries {
		p.Entries[i].PresetID = p.ID
		p.Entries[i].PresetVersion = ProviderPresetVersion
	}
	return p
}

func cloneProviderEntries(in []ProviderEntry) []ProviderEntry {
	out := make([]ProviderEntry, 0, len(in))
	for _, e := range in {
		out = append(out, cloneProviderEntry(e))
	}
	return out
}

func cloneProviderEntry(e ProviderEntry) ProviderEntry {
	if e.WebSearch != nil {
		value := *e.WebSearch
		e.WebSearch = &value
	}
	if e.ResponsesStateful != nil {
		value := *e.ResponsesStateful
		e.ResponsesStateful = &value
	}
	if e.visionOverride != nil {
		value := *e.visionOverride
		e.visionOverride = &value
	}
	e.Models = append([]string(nil), e.Models...)
	e.VisionModels = append([]string(nil), e.VisionModels...)
	e.SupportedEfforts = append([]string(nil), e.SupportedEfforts...)
	e.Headers = cloneStringMap(e.Headers)
	e.ExtraBody = cloneAnyMap(e.ExtraBody)
	e.Price = clonePricing(e.Price)
	e.Prices = clonePricingMap(e.Prices)
	e.ModelOverrides = cloneModelOverrideMap(e.ModelOverrides)
	return e
}

func clonePricingMap(in map[string]*provider.Pricing) map[string]*provider.Pricing {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*provider.Pricing, len(in))
	for k, v := range in {
		out[k] = clonePricing(v)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAnyValue(v)
	}
	return out
}

func cloneAnyValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneAnyMap(x)
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = cloneAnyValue(x[i])
		}
		return out
	default:
		return v
	}
}

func cloneModelOverrideMap(in map[string]ProviderModelOverride) map[string]ProviderModelOverride {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ProviderModelOverride, len(in))
	for k, v := range in {
		v.SupportedEfforts = append([]string(nil), v.SupportedEfforts...)
		if v.Vision != nil {
			vision := *v.Vision
			v.Vision = &vision
		}
		out[k] = v
	}
	return out
}
