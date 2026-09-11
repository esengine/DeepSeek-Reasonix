package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSetVisionModelValidatesCapabilityAndCanonicalizesRef(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{{
		Name: "gateway", Kind: "openai", BaseURL: "http://127.0.0.1:1",
		Models: []string{"text", "vision"}, Default: "text", VisionModels: []string{"vision"},
	}}}
	if err := c.SetVisionModel("auto"); err != nil || c.Agent.VisionModel != "auto" {
		t.Fatalf("auto: err=%v value=%q", err, c.Agent.VisionModel)
	}
	if err := c.SetVisionModel("gateway/vision"); err != nil || c.Agent.VisionModel != "gateway/vision" {
		t.Fatalf("explicit: err=%v value=%q", err, c.Agent.VisionModel)
	}
	if err := c.SetVisionModel("gateway/text"); err == nil {
		t.Fatal("text-only model was accepted as vision model")
	}
}

func TestRemoveProviderClearsVisionModel(t *testing.T) {
	c := &Config{Providers: []ProviderEntry{
		{Name: "text", Kind: "openai", BaseURL: "https://text.invalid", Model: "chat"},
		{Name: "vision", Kind: "openai", BaseURL: "https://vision.invalid", Model: "see", Vision: true},
	}, DefaultModel: "text", Agent: AgentConfig{VisionModel: "vision/see"}}
	if err := c.RemoveProvider("vision"); err != nil {
		t.Fatalf("RemoveProvider: %v", err)
	}
	if c.Agent.VisionModel != "" {
		t.Fatalf("vision model = %q, want cleared", c.Agent.VisionModel)
	}
}

func TestVisionModelRoundTripsThroughTOML(t *testing.T) {
	c := Default()
	c.Agent.VisionModel = "auto"
	var decoded Config
	if _, err := toml.Decode(RenderTOML(c), &decoded); err != nil {
		t.Fatalf("decode rendered config: %v", err)
	}
	if decoded.Agent.VisionModel != "auto" {
		t.Fatalf("vision_model = %q, want auto", decoded.Agent.VisionModel)
	}
}

func TestVisionCapabilityDistinguishesUnknownAndExplicitTextOnly(t *testing.T) {
	unknown := &ProviderEntry{Name: "gateway", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "opaque-chat"}
	if got := VisionCapabilityForModel(unknown); got != VisionCapabilityUnknown {
		t.Fatalf("unknown capability = %q, want %q", got, VisionCapabilityUnknown)
	}

	textOnly := *unknown
	textOnly.VisionModels = []string{}
	if got := VisionCapabilityForModel(&textOnly); got != VisionCapabilityUnsupported {
		t.Fatalf("explicit text-only capability = %q, want %q", got, VisionCapabilityUnsupported)
	}

	vision := *unknown
	vision.ModelOverrides = map[string]ProviderModelOverride{
		"opaque-chat": {Vision: boolPointer(true)},
	}
	resolved := vision
	resolved.applyModelOverride()
	if got := VisionCapabilityForModel(&resolved); got != VisionCapabilitySupported {
		t.Fatalf("model override capability = %q, want %q", got, VisionCapabilitySupported)
	}
}

func TestModelScopePresetDeclaresVerifiedVisionModels(t *testing.T) {
	preset, ok := CuratedProviderPreset("modelscope")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("modelscope preset = %+v, found=%v", preset, ok)
	}
	entry := preset.Entries[0]
	for _, model := range []string{"Qwen/Qwen3.5-397B-A17B", "Qwen/Qwen3.5-122B-A10B", "Qwen/Qwen3.5-27B"} {
		resolved := entry
		resolved.Model = model
		resolved.applyModelOverride()
		if got := VisionCapabilityForModel(&resolved); got != VisionCapabilitySupported {
			t.Fatalf("ModelScope %q capability = %q, want %q", model, got, VisionCapabilitySupported)
		}
	}
	text := entry
	text.Model = "ZhipuAI/GLM-5.2"
	if got := VisionCapabilityForModel(&text); got != VisionCapabilityUnknown {
		t.Fatalf("ModelScope GLM capability = %q, want %q", got, VisionCapabilityUnknown)
	}
}
