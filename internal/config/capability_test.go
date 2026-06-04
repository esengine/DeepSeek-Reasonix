package config

import (
	"testing"
)

func TestCapability_DeepSeekBuiltin(t *testing.T) {
	e := &ProviderEntry{Name: "ds", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4"}
	caps := EffortCapabilityForEntry(e)
	if !caps.Supported {
		t.Fatal("DeepSeek should be supported")
	}
	if len(caps.Levels) != 3 || caps.Levels[0] != "auto" || caps.Levels[1] != "high" || caps.Levels[2] != "max" {
		t.Errorf("DeepSeek levels = %v, want [auto high max]", caps.Levels)
	}
	if caps.Default != "auto" {
		t.Errorf("DeepSeek default = %q, want auto", caps.Default)
	}
}

func TestCapability_AnthropicBuiltin(t *testing.T) {
	e := &ProviderEntry{Name: "claude", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-20250514"}
	caps := EffortCapabilityForEntry(e)
	if !caps.Supported {
		t.Fatal("Anthropic should be supported")
	}
	if len(caps.Levels) != 6 {
		t.Errorf("Anthropic levels count = %d, want 6", len(caps.Levels))
	}
	if caps.Default != "auto" {
		t.Errorf("Anthropic default = %q, want auto", caps.Default)
	}
}

func TestCapability_CustomWithSupportedEfforts(t *testing.T) {
	e := &ProviderEntry{
		Name:             "mimo",
		Kind:             "openai",
		BaseURL:          "https://api.xiaomimimo.com/v1",
		Model:            "mimo-v2.5-pro",
		SupportedEfforts: []string{"auto", "low", "medium", "high"},
		DefaultEffort:    "auto",
	}
	caps := EffortCapabilityForEntry(e)
	if !caps.Supported || len(caps.Levels) != 4 {
		t.Fatalf("MiMo caps = %+v, want 4 levels", caps)
	}
	if caps.Default != "auto" {
		t.Errorf("MiMo default = %q, want auto", caps.Default)
	}
}

func TestCapability_CustomOverridesBuiltin(t *testing.T) {
	// DeepSeek entry with explicit SupportedEfforts should use config, not builtin.
	e := &ProviderEntry{
		Name:             "ds",
		Kind:             "openai",
		BaseURL:          "https://api.deepseek.com",
		Model:            "deepseek-v4",
		SupportedEfforts: []string{"auto", "low", "medium", "high"},
		DefaultEffort:    "medium",
	}
	caps := EffortCapabilityForEntry(e)
	if len(caps.Levels) != 4 || caps.Default != "medium" {
		t.Errorf("Overridden DeepSeek caps = %+v, want 4 levels, default=medium", caps)
	}
}

func TestCapability_GenericOpenAINotSupported(t *testing.T) {
	e := &ProviderEntry{Name: "gpt", Kind: "openai", BaseURL: "https://api.openai.com", Model: "gpt-4"}
	caps := EffortCapabilityForEntry(e)
	if caps.Supported {
		t.Errorf("Generic OpenAI should not be supported, got %+v", caps)
	}
}

func TestCapability_NilEntry(t *testing.T) {
	caps := EffortCapabilityForEntry(nil)
	if caps.Supported {
		t.Errorf("nil entry should not be supported, got %+v", caps)
	}
}

func TestCapability_DefaultEffortEmpty(t *testing.T) {
	e := &ProviderEntry{
		Name:             "custom",
		Kind:             "openai",
		SupportedEfforts: []string{"low", "high"},
		DefaultEffort:    "",
	}
	caps := EffortCapabilityForEntry(e)
	if caps.Default != "auto" {
		t.Errorf("empty DefaultEffort should resolve to auto, got %q", caps.Default)
	}
}
