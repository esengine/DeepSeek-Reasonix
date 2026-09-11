package config

import (
	"reflect"
	"testing"
)

func TestCuratedProviderPresetsSCNetUsesOfficialBaseURLs(t *testing.T) {
	wantModels := []string{
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
	wantVisionModels := []string{"Kimi-K2.6", "Kimi-K2.5"}
	tests := []struct {
		id           string
		kind         string
		baseURL      string
		modelsURL    string
		models       []string
		visionModels []string
		authHeader   bool
	}{
		{
			id:           "scnet",
			kind:         "openai",
			baseURL:      "https://api.scnet.cn/api/llm/v1",
			modelsURL:    "https://api.scnet.cn/api/llm/v1/models",
			models:       wantModels,
			visionModels: wantVisionModels,
		},
		{
			id:           "scnet-anthropic",
			kind:         "anthropic",
			baseURL:      "https://api.scnet.cn/api/llm/anthropic",
			models:       wantModels,
			visionModels: wantVisionModels,
			authHeader:   true,
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
			if !reflect.DeepEqual(entry.Models, tt.models) {
				t.Fatalf("preset %q models = %q, want %q", tt.id, entry.Models, tt.models)
			}
			if !reflect.DeepEqual(entry.VisionModels, tt.visionModels) {
				t.Fatalf("preset %q vision_models = %q, want %q", tt.id, entry.VisionModels, tt.visionModels)
			}
			if entry.DefaultModel() != "MiniMax-M2.5" {
				t.Fatalf("preset %q default = %q, want MiniMax-M2.5", tt.id, entry.DefaultModel())
			}
			if tt.kind == "anthropic" && entry.Thinking != "" {
				t.Fatalf("preset %q thinking = %q, want omitted", tt.id, entry.Thinking)
			}
			var cfg Config
			if err := cfg.UpsertProvider(entry); err != nil {
				t.Fatalf("preset %q entry failed validation: %v", tt.id, err)
			}
		})
	}
}

func TestCuratedProviderPresetSCNetAppliesPerModelCapabilities(t *testing.T) {
	preset, ok := CuratedProviderPreset("scnet")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("SCNet preset = %+v", preset)
	}
	var cfg Config
	if err := cfg.UpsertProvider(preset.Entries[0]); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}

	deepseek, ok := cfg.ResolveModel("scnet/DeepSeek-V4-Flash")
	if !ok {
		t.Fatal("ResolveModel(scnet/DeepSeek-V4-Flash) failed")
	}
	if got := ReasoningProtocolForEntry(deepseek); got != ReasoningProtocolOpenAI {
		t.Fatalf("DeepSeek-V4-Flash reasoning protocol = %q, want %q", got, ReasoningProtocolOpenAI)
	}
	if !reflect.DeepEqual(deepseek.SupportedEfforts, []string{"high", "max"}) || deepseek.DefaultEffort != "high" {
		t.Fatalf("DeepSeek-V4-Flash efforts = %v/%q, want [high max]/high", deepseek.SupportedEfforts, deepseek.DefaultEffort)
	}

	vision, ok := cfg.ResolveModel("scnet/Kimi-K2.5")
	if !ok || !EffectiveVision(vision) {
		t.Fatalf("Kimi-K2.5 vision capability = %t, found=%t", EffectiveVision(vision), ok)
	}
	textOnly, ok := cfg.ResolveModel("scnet/MiniMax-M2.5")
	if !ok || EffectiveVision(textOnly) {
		t.Fatalf("MiniMax-M2.5 vision capability = %t, found=%t", EffectiveVision(textOnly), ok)
	}
}
