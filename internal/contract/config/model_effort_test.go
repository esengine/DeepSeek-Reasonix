package config

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

// mixedRelay serves three vendors' models under one key: one declares its own
// levels, one inherits the connection's, and one is on a connection with no
// declaration at all, where the model table answers.
func mixedRelay() *Config {
	cfg := &Config{Providers: []ProviderEntry{
		{
			Name: "relay", Kind: "openai", BaseURL: "https://relay.example.com/v1",
			Models: []string{"claude-opus-9", "qwen-relay", "unknown-model"}, ReasoningProtocol: ReasoningProtocolOpenAI,
			SupportedEfforts: []string{"low", "medium", "high"}, DefaultEffort: "high",
			ModelOverrides: map[string]ProviderModelOverride{
				"claude-opus-9": {SupportedEfforts: []string{"low", "max"}, DefaultEffort: "max", ContextWindow: 400_000},
			},
		},
		{
			Name: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1",
			Models: []string{"gpt-5.6-sol", "no-ladder-model"},
		},
		{
			Name: "gw", Kind: "openai", BaseURL: "https://gw.example.com/v1",
			Models: []string{"unknown-model"}, ReasoningProtocol: ReasoningProtocolOpenAI,
		},
	}}
	normalizeEffortConfig(cfg)
	return cfg
}

func resolvedLadder(t *testing.T, cfg *Config, ref string) (EffortCapability, []string) {
	t.Helper()
	e, ok := cfg.ResolveModel(ref)
	if !ok {
		t.Fatalf("%s does not resolve", ref)
	}
	return EffortCapabilityForEntry(e), RequestEffortLevels(e)
}

// Model's own levels, then the connection's, then the model table for the
// endpoint, then nothing; the picker and the request read the same answer.
func TestModelEffortPrecedence(t *testing.T) {
	cfg := mixedRelay()
	cases := []struct {
		ref     string
		offered []string
		def     string
	}{
		{"relay/claude-opus-9", []string{"auto", "low", "max"}, "max"},
		{"relay/qwen-relay", []string{"auto", "low", "medium", "high"}, "high"},
		{"openai/gpt-5.6-sol", []string{"auto", "none", "low", "medium", "high", "xhigh", "max"}, "medium"},
	}
	for _, tc := range cases {
		menu, wire := resolvedLadder(t, cfg, tc.ref)
		if !slices.Equal(menu.Levels, tc.offered) || menu.Default != tc.def {
			t.Fatalf("%s offers %v default %q, want %v default %q", tc.ref, menu.Levels, menu.Default, tc.offered, tc.def)
		}
		if !slices.Equal(wire, tc.offered[1:]) {
			t.Fatalf("%s sends from %v but offers %v", tc.ref, wire, tc.offered)
		}
	}
	if menu, _ := resolvedLadder(t, cfg, "gw/unknown-model"); !slices.Equal(menu.Levels, []string{"auto", "low", "medium", "high"}) {
		t.Fatalf("a model nothing declares offers %v, want the protocol's generic ladder", menu.Levels)
	}
	if menu, wire := resolvedLadder(t, cfg, "openai/no-ladder-model"); menu.Supported || wire != nil {
		t.Fatalf("a model with no protocol and no ladder offers %+v and sends from %v, want neither", menu, wire)
	}
}

func TestInheritedEffortCapabilityIgnoresTheModelsOwnLevels(t *testing.T) {
	cfg := mixedRelay()
	relay, _ := cfg.Provider("relay")
	got := relay.InheritedEffortCapability("CLAUDE-OPUS-9")
	if !slices.Equal(got.Levels, []string{"auto", "low", "medium", "high"}) || got.Default != "high" {
		t.Fatalf("inherited = %+v, want the connection's levels", got)
	}
	if own, ok := relay.ModelEffortDeclaration("claude-opus-9"); !ok || !slices.Equal(own.Levels, []string{"low", "max"}) {
		t.Fatalf("inheriting must not touch the stored declaration: %+v", own)
	}
}

func TestSetModelEffortDeclarationKeepsTheRestOfTheOverride(t *testing.T) {
	cfg := mixedRelay()
	relay, _ := cfg.Provider("relay")
	if err := relay.SetModelEffortDeclaration("Claude-Opus-9", []string{"High", "auto", "xhigh"}, "XHIGH"); err != nil {
		t.Fatal(err)
	}
	ov := relay.ModelOverrides["claude-opus-9"]
	if len(relay.ModelOverrides) != 1 || !slices.Equal(ov.SupportedEfforts, []string{"high", "xhigh"}) || ov.DefaultEffort != "xhigh" || ov.ContextWindow != 400_000 {
		t.Fatalf("override after set = %+v (all: %+v)", ov, relay.ModelOverrides)
	}
	if err := relay.SetModelEffortDeclaration("claude-opus-9", nil, "max"); err != nil {
		t.Fatal(err)
	}
	if ov := relay.ModelOverrides["claude-opus-9"]; ov.SupportedEfforts != nil || ov.DefaultEffort != "" || ov.ContextWindow != 400_000 {
		t.Fatalf("clearing the levels must keep the window and drop the default: %+v", ov)
	}
	if err := relay.SetModelEffortDeclaration("qwen-relay", []string{"low"}, "high"); !errors.Is(err, ErrModelDefaultEffortNotListed) {
		t.Fatalf("a default outside the levels = %v, want ErrModelDefaultEffortNotListed", err)
	}
	if err := relay.SetModelEffortDeclaration("qwen-relay", nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := relay.ModelOverrides["qwen-relay"]; ok {
		t.Fatal("clearing a model with nothing else declared must leave no empty override behind")
	}
}

func TestModelEffortDeclarationSurvivesSaveAndLoad(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "config.toml")
	cfg := LoadForEdit(path)
	cfg.Providers = mixedRelay().Providers
	relay, _ := cfg.Provider("relay")
	if err := relay.SetModelEffortDeclaration("qwen-relay", []string{"none", "xhigh"}, "xhigh"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToScope(path, RenderScopeUser); err != nil {
		t.Fatal(err)
	}

	back := LoadForEdit(path)
	menu, wire := resolvedLadder(t, back, "relay/qwen-relay")
	if !slices.Equal(menu.Levels, []string{"auto", "none", "xhigh"}) || menu.Default != "xhigh" || !slices.Equal(wire, []string{"none", "xhigh"}) {
		t.Fatalf("after the round trip qwen-relay offers %+v and sends from %v", menu, wire)
	}
	if menu, _ := resolvedLadder(t, back, "relay/claude-opus-9"); !slices.Equal(menu.Levels, []string{"auto", "low", "max"}) {
		t.Fatalf("the untouched model lost its levels: %v", menu.Levels)
	}

	relay, _ = back.Provider("relay")
	if err := relay.SetModelEffortDeclaration("qwen-relay", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := back.SaveToScope(path, RenderScopeUser); err != nil {
		t.Fatal(err)
	}
	again := LoadForEdit(path)
	if menu, _ := resolvedLadder(t, again, "relay/qwen-relay"); !slices.Equal(menu.Levels, []string{"auto", "low", "medium", "high"}) {
		t.Fatalf("a cleared model offers %v, want the connection's levels again", menu.Levels)
	}
}

// An override that only caps output is a declaration like any other.
func TestModelOverrideWithOnlyAnOutputCapSurvivesNormalization(t *testing.T) {
	got := normalizedModelOverrides(map[string]ProviderModelOverride{"m": {MaxOutputTokens: 8192}})
	if got["m"].MaxOutputTokens != 8192 {
		t.Fatalf("normalized overrides = %+v, want the output cap kept", got)
	}
}

// The stored level is one per connection, so switching models can leave it
// outside the next model's levels; that model then runs at its own default,
// the level its picker's auto names, never at the stored one or at nothing.
func TestStoredEffortOutsideAModelsLevelsRunsAtItsDefault(t *testing.T) {
	cfg := mixedRelay()
	relay, _ := cfg.Provider("relay")
	relay.Effort = "max"
	claude, _ := cfg.ResolveModel("relay/claude-opus-9")
	qwen, _ := cfg.ResolveModel("relay/qwen-relay")
	if got := EffectiveEffort(claude); got != "max" {
		t.Fatalf("claude-opus-9 declares max and runs at %q", got)
	}
	if got, shown := EffectiveEffort(qwen), EffortDisplay(qwen); got != "high" || shown != "auto" {
		t.Fatalf("qwen-relay runs at %q showing %q, want its default high under auto", got, shown)
	}
}
