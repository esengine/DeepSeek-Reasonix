package config

// The vendor documents "1M" for every MiMo text model without saying which; the
// decimal figure is the conservative reading. legacyMimoWindow is what the
// presets shipped before and identifies their shape.
const (
	mimoContextWindow = 1_000_000
	legacyMimoWindow  = 1_048_576
)

var (
	mimoModels       = []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.5-pro", "mimo-v2.5"}
	mimoPAYGModels   = []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed", "mimo-v2.5-pro", "mimo-v2.5"}
	mimoVisionList   = []string{"mimo-v2.5"}
	legacyMimoModels = []string{"mimo-v2.5-pro", "mimo-v2.5"}
	legacyMimoVision = []string{"mimo-v2.5"}
)

var mimoPresets = []ProviderPreset{
	{
		ID:          "mimo-api",
		Label:       "MiMo API",
		Description: "Xiaomi MiMo direct API with text and vision-capable models.",
		KeyEnv:      "MIMO_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "mimo-api",
			Kind:          "openai",
			BaseURL:       "https://api.xiaomimimo.com/v1",
			Models:        mimoPAYGModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoPAYGModels),
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
			Models:        mimoPAYGModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoPAYGModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
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
			Models:        mimoModels,
			VisionModels:  mimoVisionList,
			Default:       mimoModels[0],
			APIKeyEnv:     "MIMO_TOKEN_PLAN_API_KEY",
			Thinking:      "adaptive",
			ContextWindow: mimoContextWindow,
			Prices:        mimoDomesticPrices(mimoModels),
		}},
	},
}
