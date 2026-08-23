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
