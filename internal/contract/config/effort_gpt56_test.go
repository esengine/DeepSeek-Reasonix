package config

import (
	"slices"
	"testing"
)

// A relay serving these models under its own name is the case that has to
// work: nothing about such an entry is declared, so without a model-keyed
// registry entry it resolves to no capability and the composer shows no
// control at all — which is what shipped before this.
func TestGPT56LadderReachesAnUndeclaredRelay(t *testing.T) {
	relay := &ProviderEntry{
		Name: "relay", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "gpt-5.6-sol",
	}
	got := EffortCapabilityForEntry(relay)
	if !got.Supported {
		t.Fatal("a relay serving gpt-5.6-sol still has no effort ladder")
	}
	// Minus xhigh and max: those are the vendor's own extensions, and this
	// endpoint has never been measured against them.
	want := []string{"auto", "none", "low", "medium", "high"}
	if !slices.Equal(got.Levels, want) {
		t.Fatalf("levels = %v, want %v", got.Levels, want)
	}
	if got.Default != "medium" {
		t.Fatalf("default = %q, want medium", got.Default)
	}
}

// The endpoint refuses "minimal" by naming the model, while the generic API
// vocabulary carries it. An alias degrades rather than sending a level the
// model will reject.
func TestGPT56MinimalDegradesInsteadOfBeingSent(t *testing.T) {
	relay := &ProviderEntry{
		Name: "relay", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "gpt-5.6-luna",
	}
	got, err := NormalizeEffort(relay, "minimal")
	if err != nil {
		t.Fatalf("NormalizeEffort(minimal) = %v, want it to degrade", err)
	}
	if got != "low" {
		t.Fatalf("minimal resolved to %q, want low", got)
	}
	if v, err := NormalizeEffort(relay, "none"); err != nil || v != "none" {
		t.Fatalf("NormalizeEffort(none) = %q, %v — want it accepted as itself", v, err)
	}
	// The vendor's extensions degrade for the same reason minimal does: asking
	// for more depth than this endpoint's vocabulary carries is answered with
	// the most it does, not with a level it rejects.
	for _, lvl := range []string{"xhigh", "max"} {
		if v, err := NormalizeEffort(relay, lvl); err != nil || v != "high" {
			t.Fatalf("NormalizeEffort(%q) = %q, %v — want it degraded to high", lvl, v, err)
		}
	}
	if _, err := NormalizeEffort(relay, "banana"); err == nil {
		t.Fatal("a level the endpoint would reject was accepted")
	}
}

// The Claude models this same relay serves stay without a ladder: measured, the
// field is accepted and changes nothing there, and a control that changes
// nothing is worse than none.
func TestClaudeOnAnOpenAIRelayStillHasNoLadder(t *testing.T) {
	relay := &ProviderEntry{
		Name: "relay", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "claude-opus-5",
	}
	if EffortCapabilityForEntry(relay).Supported {
		t.Fatal("claude on the OpenAI wire gained a ladder it cannot act on")
	}
}
