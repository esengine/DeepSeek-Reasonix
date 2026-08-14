package config

import "testing"

func TestCuratedProviderPresetsSCNetUsesOfficialBaseURLs(t *testing.T) {
	tests := []struct {
		id         string
		kind       string
		baseURL    string
		modelsURL  string
		authHeader bool
	}{
		{
			id:        "scnet",
			kind:      "openai",
			baseURL:   "https://api.scnet.cn/api/llm/v1",
			modelsURL: "https://api.scnet.cn/api/llm/v1/models",
		},
		{
			id:         "scnet-anthropic",
			kind:       "anthropic",
			baseURL:    "https://api.scnet.cn/api/llm/anthropic",
			modelsURL:  "https://api.scnet.cn/api/llm/v1/models",
			authHeader: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			preset, ok := CuratedProviderPreset(tt.id)
			if !ok {
				t.Fatalf("missing preset %q", tt.id)
			}
			if preset.KeyEnv != "SCNET_API_KEY" {
				t.Fatalf("preset %q key_env = %q, want SCNET_API_KEY", tt.id, preset.KeyEnv)
			}
			if len(preset.Entries) != 1 {
				t.Fatalf("preset %q has %d entries, want 1", tt.id, len(preset.Entries))
			}
			entry := preset.Entries[0]
			if entry.Kind != tt.kind {
				t.Fatalf("preset %q kind = %q, want %q", tt.id, entry.Kind, tt.kind)
			}
			if entry.BaseURL != tt.baseURL {
				t.Fatalf("preset %q base_url = %q, want %q", tt.id, entry.BaseURL, tt.baseURL)
			}
			if entry.ModelsURL != tt.modelsURL {
				t.Fatalf("preset %q models_url = %q, want %q", tt.id, entry.ModelsURL, tt.modelsURL)
			}
			if entry.AuthHeader != tt.authHeader {
				t.Fatalf("preset %q auth_header = %t, want %t", tt.id, entry.AuthHeader, tt.authHeader)
			}
			if entry.DefaultModel() != "MiniMax-M2.5" {
				t.Fatalf("preset %q default = %q, want MiniMax-M2.5", tt.id, entry.DefaultModel())
			}
			if tt.kind == "anthropic" && entry.Thinking != "adaptive" {
				t.Fatalf("preset %q thinking = %q, want adaptive", tt.id, entry.Thinking)
			}
			var cfg Config
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("preset %q entry failed validation: %v", tt.id, err)
			}
		})
	}
}
