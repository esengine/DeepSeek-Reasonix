package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKimiK3ReasoningProtocolControlsEffortCapability(t *testing.T) {
	e := &ProviderEntry{
		Name: "custom", Kind: "openai", BaseURL: "https://example.com", Model: "kimi-k3",
		ReasoningProtocol: ReasoningProtocolKimiK3,
		SupportedEfforts:  []string{"medium", "ultra"}, DefaultEffort: "ultra", Effort: "ultra",
	}
	cap := EffortCapabilityForEntry(e)
	wantLevels := []string{"auto", "low", "high", "max"}
	if !cap.Supported || cap.Default != "max" || !reflect.DeepEqual(cap.Levels, wantLevels) {
		t.Fatalf("reasoning_protocol=kimi-k3 capability = %+v, want levels %v with default max", cap, wantLevels)
	}
	if protocol := ReasoningProtocolForEntry(e); protocol != ReasoningProtocolKimiK3 {
		t.Fatalf("protocol = %q, want kimi-k3", protocol)
	}
	for _, level := range []string{"low", "high", "max"} {
		if got, err := NormalizeEffort(e, level); err != nil || got != level {
			t.Fatalf("NormalizeEffort(%s) = %q/%v, want %s/nil", level, got, err, level)
		}
	}
	if _, err := NormalizeEffort(e, "medium"); err == nil {
		t.Fatal("Kimi K3 reasoning_protocol should reject medium")
	}
	if _, err := NormalizeEffort(e, "ultra"); err == nil {
		t.Fatal("Kimi K3 reasoning_protocol should ignore custom supported_efforts")
	}
	if got := EffortDisplay(e); got != "auto" {
		t.Fatalf("invalid stored Kimi K3 effort display = %q, want auto", got)
	}
	if got := EffectiveEffort(e); got != "" {
		t.Fatalf("invalid stored Kimi K3 effective effort = %q, want provider default", got)
	}
	e.Effort = "high"
	if got := EffortDisplay(e); got != "high" {
		t.Fatalf("valid stored Kimi K3 effort display = %q, want high", got)
	}
	if got := EffectiveEffort(e); got != "high" {
		t.Fatalf("valid stored Kimi K3 effective effort = %q, want high", got)
	}
}

func TestKimiK3ReasoningProtocolPreservesDormantCustomEfforts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := &Config{}
	wantSupported := []string{"medium", "ultra"}
	if err := c.UpsertProvider(ProviderEntry{
		Name: "custom-kimi-gateway", Kind: "openai", BaseURL: "https://gateway.example.com/v1", Model: "kimi-k3",
		ReasoningProtocol: ReasoningProtocolKimiK3, SupportedEfforts: wantSupported, DefaultEffort: "ultra",
	}); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded := LoadForEditWithoutCredentials(path)
	got, ok := loaded.Provider("custom-kimi-gateway")
	if !ok {
		t.Fatal("saved Kimi K3 provider was not reloaded")
	}
	if !reflect.DeepEqual(got.SupportedEfforts, wantSupported) || got.DefaultEffort != "ultra" {
		t.Fatalf("dormant custom efforts were not preserved: %+v", got)
	}
	cap := EffortCapabilityForEntry(got)
	if want := []string{"auto", "low", "high", "max"}; !reflect.DeepEqual(cap.Levels, want) || cap.Default != "max" {
		t.Fatalf("reloaded Kimi K3 capability = %+v, want fixed protocol defaults", cap)
	}
}

func TestKimiK3ReasoningProtocolRoundTripsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	c := &Config{}
	if err := c.UpsertProvider(ProviderEntry{
		Name: "custom-kimi-gateway", Kind: "openai", BaseURL: "https://gateway.example.com/v1", Model: "kimi-k3",
		ReasoningProtocol: " KIMI-K3 ", SupportedEfforts: []string{"low", "high", "max"}, DefaultEffort: "max",
	}); err != nil {
		t.Fatalf("UpsertProvider: %v", err)
	}
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	loaded := LoadForEditWithoutCredentials(path)
	got, ok := loaded.Provider("custom-kimi-gateway")
	if !ok {
		t.Fatal("saved Kimi K3 provider was not reloaded")
	}
	if got.ReasoningProtocol != ReasoningProtocolKimiK3 || !reflect.DeepEqual(got.SupportedEfforts, []string{"low", "high", "max"}) || got.DefaultEffort != "max" {
		t.Fatalf("reloaded Kimi K3 provider = %+v", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(body), `reasoning_protocol = "kimi-k3"`) {
		t.Fatalf("saved config lost Kimi K3 protocol:\n%s", body)
	}
}
