package config

// The native preset lists only models whose own pages were read on 2026-10-17
// (https://ai.google.dev/gemini-api/docs/models/<model>), with the thinking levels
// from https://ai.google.dev/gemini-api/docs/thinking. Experimental: not run
// against the real endpoint.
const geminiNativeWindow = 1_048_576

var geminiNativeModels = []string{"gemini-3.8-flash", "gemini-4-argon", "gemini-3.6-flash", "gemini-3.5-flash-lite", "gemini-3.1-pro-preview"}

var geminiNativePresets = []ProviderPreset{
	{
		ID:          "gemini-native",
		Vendor:      "google",
		Label:       "Google Gemini, native (experimental)",
		Description: "Google's Gemini API through its Interactions API, with native video. Experimental: built from Google's documentation and not yet run against the real endpoint.",
		KeyEnv:      "GEMINI_API_KEY",
		Entries: []ProviderEntry{{
			Name:          "gemini-native",
			Kind:          "gemini",
			BaseURL:       "https://generativelanguage.googleapis.com/v1beta",
			Models:        geminiNativeModels,
			VisionModels:  geminiNativeModels,
			VideoModels:   geminiNativeModels,
			VideoProtocol: VideoProtocolGeminiFiles,
			Default:       "gemini-3.8-flash",
			APIKeyEnv:     "GEMINI_API_KEY",
			ContextWindow: geminiNativeWindow,
			Prices:        officialVendorPrices("google", "USD", geminiNativeModels),
			ModelOverrides: map[string]ProviderModelOverride{
				"gemini-3.8-flash":       {SupportedEfforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
				"gemini-4-argon":         {SupportedEfforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
				"gemini-3.6-flash":       {SupportedEfforts: []string{"minimal", "low", "medium", "high"}, DefaultEffort: "medium"},
				"gemini-3.5-flash-lite":  {SupportedEfforts: []string{"minimal", "low", "medium", "high"}, DefaultEffort: "minimal"},
				"gemini-3.1-pro-preview": {SupportedEfforts: []string{"low", "medium", "high"}, DefaultEffort: "high"},
			},
		}},
	},
}
