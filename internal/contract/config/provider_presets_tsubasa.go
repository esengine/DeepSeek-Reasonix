package config

func tsubasaPreset() ProviderPreset {
	return ProviderPreset{
		ID:          "tsubasa",
		Label:       "Tsubasa",
		Description: "Tsubasa OpenAI-compatible Chat Completions endpoint.",
		KeyEnv:      "TSUBASA_API_KEY",
		Entries: []ProviderEntry{{
			Name:              "tsubasa",
			Kind:              "openai",
			BaseURL:           "https://api.tsubasa.sh/v1",
			Models:            []string{"tsubasa-pro", "tsubasa-fast"},
			Default:           "tsubasa-pro",
			APIKeyEnv:         "TSUBASA_API_KEY",
			ContextWindow:     32768,
			MaxOutputTokens:   8192,
			ReasoningProtocol: ReasoningProtocolNone,
		}},
	}
}
