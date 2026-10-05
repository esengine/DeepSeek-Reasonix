package config

import (
	"strings"
	"testing"
)

func mimoEntry() *ProviderEntry {
	return &ProviderEntry{
		Name: "xiaomimimo", Kind: "openai",
		BaseURL: "https://api.xiaomimimo.com/v1",
		Model:   "mimo-v2.5-pro",
	}
}

// Measured: none|low|medium|high answer 200, max|xhigh answer 400. Inferring
// the protocol skipped MiMo, so a stock config reported no levels at all and
// refused every effort switch.
func TestMimoOffersItsLevelsWithoutADeclaredProtocol(t *testing.T) {
	entry := mimoEntry()
	capability := EffortCapabilityForEntry(entry)
	if !capability.Supported {
		t.Fatalf("a stock MiMo provider reports no effort levels: %+v", capability)
	}
	want := "auto | none | low | medium | high"
	if got := strings.Join(capability.Levels, " | "); got != want {
		t.Errorf("levels = %q, want %q", got, want)
	}
	for _, level := range []string{"none", "low", "medium", "high"} {
		got, err := NormalizeEffort(entry, level)
		if err != nil || got != level {
			t.Errorf("NormalizeEffort(%q) = (%q, %v), want it accepted as-is", level, got, err)
		}
	}
	// The endpoint answers 400 to these, so the host must refuse them before
	// spending a request to find out.
	for _, level := range []string{"max", "xhigh"} {
		if _, err := NormalizeEffort(entry, level); err == nil {
			t.Errorf("NormalizeEffort(%q) was accepted, but the endpoint rejects it", level)
		}
	}
}

// The shape of the defect: two switches naming one protocol's levels, and only
// one knowing about MiMo. Declared and inferred must agree.
func TestDeclaredAndInferredProtocolsAgree(t *testing.T) {
	inferred := mimoEntry()
	declared := mimoEntry()
	declared.ReasoningProtocol = ReasoningProtocolOpenAI

	got := EffortCapabilityForEntry(inferred)
	want := EffortCapabilityForEntry(declared)
	if strings.Join(got.Levels, "|") != strings.Join(want.Levels, "|") || got.Default != want.Default {
		t.Fatalf("inferred %+v differs from declared %+v", got, want)
	}
}
