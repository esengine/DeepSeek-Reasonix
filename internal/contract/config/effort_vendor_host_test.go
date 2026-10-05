package config

import (
	"slices"
	"testing"
)

// The reported failure: a relay serving deepseek-v4-flash was offered "max" —
// the vendor's own extension — and answered every turn with "field
// ReasoningEffort invalid, should be one of: low, medium, high, xhigh, none"
// until the setting was changed by hand. The levels an API takes belong to the
// endpoint; only the model and its window travel with the model name.
func TestRelayIsNotOfferedTheVendorsExtension(t *testing.T) {
	relay := &ProviderEntry{
		Name: "third-party", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "deepseek-v4-flash",
	}
	cap := EffortCapabilityForEntry(relay)
	if !cap.Supported {
		t.Fatal("the relay lost its effort control entirely; the ladder should narrow, not vanish")
	}
	if slices.Contains(cap.Levels, "max") {
		t.Fatalf("levels = %v, still offering the vendor extension that 400s here", cap.Levels)
	}
	if got, err := NormalizeEffort(relay, "max"); err != nil || got != "high" {
		t.Fatalf("NormalizeEffort(max) = %q/%v, want it degraded to high", got, err)
	}
	// The model still decides what it is and how much it holds.
	if protocol := ReasoningProtocolForEntry(relay); protocol != ReasoningProtocolDeepSeek {
		t.Fatalf("protocol = %q, want deepseek — that one does travel with the model", protocol)
	}
	if window, ok := ResolvedContextWindow(relay); !ok || window != 1_000_000 {
		t.Fatalf("context window = %d/%v, want the model's own 1M", window, ok)
	}
}

// A declared protocol with no model-table entry reaches the fallback ladder,
// which is the other half of the same question. (An undeclared unknown model on
// a relay resolves to no protocol at all and has no control to narrow.)
func TestRelayFallbackIsNotOfferedTheVendorsExtension(t *testing.T) {
	relay := &ProviderEntry{
		Name: "third-party", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "deepseek-v4-something-new",
		ReasoningProtocol: ReasoningProtocolDeepSeek,
	}
	cap := EffortCapabilityForEntry(relay)
	if slices.Contains(cap.Levels, "max") {
		t.Fatalf("levels = %v, still offering max on an unmeasured endpoint", cap.Levels)
	}
	if got, err := NormalizeEffort(relay, "xhigh"); err != nil || got != "high" {
		t.Fatalf("NormalizeEffort(xhigh) = %q/%v, want it degraded to high", got, err)
	}
}

// The other direction, so narrowing the relay costs the vendor nothing: both a
// table model and one the table has never heard of keep max on the vendor host.
func TestVendorEndpointKeepsItsFullLadder(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash", "deepseek-v4-something-new"} {
		vendor := &ProviderEntry{
			Name: "deepseek", Kind: "openai",
			BaseURL: "https://api.deepseek.com/v1", Model: model,
			ReasoningProtocol: ReasoningProtocolDeepSeek,
		}
		cap := EffortCapabilityForEntry(vendor)
		if !slices.Contains(cap.Levels, "max") {
			t.Errorf("%s on the vendor host lost max: levels = %v", model, cap.Levels)
		}
		if got, err := NormalizeEffort(vendor, "max"); err != nil || got != "max" {
			t.Errorf("%s: NormalizeEffort(max) = %q/%v, want max", model, got, err)
		}
	}
}

// A hostname that merely contains the vendor's is not the vendor: the ladder is
// matched on the host, never on the URL as a string.
func TestLookalikeHostIsNotTheVendor(t *testing.T) {
	for _, url := range []string{
		"https://api.deepseek.com.evil.example/v1",
		"https://relay.example.com/api.deepseek.com/v1",
	} {
		e := &ProviderEntry{Name: "x", Kind: "openai", BaseURL: url, Model: "deepseek-v4-flash"}
		if cap := EffortCapabilityForEntry(e); slices.Contains(cap.Levels, "max") {
			t.Errorf("%s was treated as the vendor endpoint: levels = %v", url, cap.Levels)
		}
	}
}

// A relay that does take the extension says so, and that declaration outranks
// everything the host would otherwise assume.
func TestSupportedEffortsRestoresWhatTheHostWillNotAssume(t *testing.T) {
	relay := &ProviderEntry{
		Name: "third-party", Kind: "openai",
		BaseURL: "https://relay.example.com/v1", Model: "deepseek-v4-flash",
		SupportedEfforts: []string{"none", "low", "medium", "high", "xhigh"},
	}
	cap := EffortCapabilityForEntry(relay)
	if !slices.Contains(cap.Levels, "xhigh") {
		t.Fatalf("levels = %v, want the declared vocabulary to win", cap.Levels)
	}
	if got, err := NormalizeEffort(relay, "xhigh"); err != nil || got != "xhigh" {
		t.Fatalf("NormalizeEffort(xhigh) = %q/%v, want it sent as declared", got, err)
	}
}
