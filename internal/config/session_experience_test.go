package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDesktopSessionExperienceDefaultsAndLegacyMigration(t *testing.T) {
	for _, legacy := range []DesktopConfig{
		{DisplayMode: "compact"},
		{DisplayMode: "minimal"},
		{ReasoningDisplayMode: "hidden"},
		{ReasoningDisplayMode: "summary"},
		{ReasoningDisplayMode: "auto"},
		{ReasoningDisplayMode: "expanded", ExpandThinking: true},
	} {
		cfg := Default()
		cfg.Desktop = legacy
		if got := cfg.DesktopSessionExperience(); got != "standard" {
			t.Fatalf("legacy config %+v resolved to %q, want standard", legacy, got)
		}
	}
}

func TestSetDesktopSessionExperienceSynchronizesCompatibilityFields(t *testing.T) {
	tests := []struct {
		mode      string
		reasoning string
	}{
		{mode: "standard", reasoning: "auto"},
		{mode: "deep", reasoning: "expanded"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cfg := Default()
			if err := cfg.SetDesktopSessionExperience(tt.mode); err != nil {
				t.Fatalf("SetDesktopSessionExperience(%q): %v", tt.mode, err)
			}
			if got := cfg.DesktopSessionExperience(); got != tt.mode {
				t.Fatalf("experience = %q, want %q", got, tt.mode)
			}
			if cfg.Desktop.DisplayMode != "standard" || cfg.Desktop.ReasoningDisplayMode != tt.reasoning {
				t.Fatalf("compatibility mirrors = %+v", cfg.Desktop)
			}
			if got := cfg.DesktopReasoningDisplayMode(); got != tt.reasoning {
				t.Fatalf("reasoning mode = %q, want %q", got, tt.reasoning)
			}
		})
	}
	if err := Default().SetDesktopSessionExperience("invalid"); err == nil {
		t.Fatal("invalid session experience unexpectedly accepted")
	}
}

func TestDesktopSessionExperienceRenderRoundTrip(t *testing.T) {
	cfg := Default()
	if err := cfg.SetDesktopSessionExperience("deep"); err != nil {
		t.Fatal(err)
	}
	rendered := RenderTOML(cfg)
	if !strings.Contains(rendered, `session_experience = "deep"`) {
		t.Fatalf("rendered config missing canonical experience:\n%s", rendered)
	}
	var decoded Config
	if _, err := toml.Decode(rendered, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.DesktopSessionExperience(); got != "deep" {
		t.Fatalf("round-tripped experience = %q, want deep", got)
	}
}
