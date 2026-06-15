package serve

import (
	"testing"

	"reasonix/internal/config"
)

func TestApplyEffortEditUpsertsMissingProvider(t *testing.T) {
	edit := &config.Config{}
	entry := &config.ProviderEntry{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4"}

	if err := applyEffortEdit(edit, entry, "high"); err != nil {
		t.Fatalf("applyEffortEdit: %v", err)
	}
	got, ok := edit.Provider("deepseek")
	if !ok {
		t.Fatal("provider absent from user config should be upserted so the effort edit lands")
	}
	if got.Effort != "high" {
		t.Fatalf("effort = %q, want high", got.Effort)
	}
}

func TestApplyEffortEditEnablesAnthropicThinking(t *testing.T) {
	edit := &config.Config{}
	entry := &config.ProviderEntry{Name: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-opus-4-8"}

	if err := applyEffortEdit(edit, entry, "max"); err != nil {
		t.Fatalf("applyEffortEdit: %v", err)
	}
	got, _ := edit.Provider("anthropic")
	if got.Thinking != "adaptive" {
		t.Fatalf("thinking = %q, want adaptive (effort needs extended thinking to engage)", got.Thinking)
	}
	if got.Effort != "max" {
		t.Fatalf("effort = %q, want max", got.Effort)
	}
}

func TestApplyEffortEditKeepsExistingAnthropicThinking(t *testing.T) {
	edit := &config.Config{Providers: []config.ProviderEntry{
		{Name: "anthropic", Kind: "anthropic", Model: "claude-opus-4-8", Thinking: "always"},
	}}
	entry := &config.ProviderEntry{Name: "anthropic", Kind: "anthropic", Model: "claude-opus-4-8", Thinking: "always"}

	if err := applyEffortEdit(edit, entry, "low"); err != nil {
		t.Fatalf("applyEffortEdit: %v", err)
	}
	got, _ := edit.Provider("anthropic")
	if got.Thinking != "always" {
		t.Fatalf("thinking = %q, want it left untouched", got.Thinking)
	}
}
