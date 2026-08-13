package agentpreset

import "testing"

func TestNormalizeLegacyAndUnknown(t *testing.T) {
	cases := map[string]AgentPreset{
		"":           Balanced,
		"full":       Balanced,
		"balanced":   Balanced,
		"BALANCED":   Balanced,
		"economy":    Light,
		"eco":        Light,
		"light":      Light,
		"lite":       Light,
		"delivery":   Delivery,
		"quality":    Delivery,
		"unexpected": Balanced,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegacyTokenModeRoundTrip(t *testing.T) {
	for _, p := range All() {
		legacy := LegacyTokenMode(p)
		if got := FromLegacyTokenMode(legacy); got != p {
			t.Errorf("FromLegacyTokenMode(%q) = %q, want %q", legacy, got, p)
		}
	}
	if got := FromLegacyTokenMode("economy"); got != Light {
		t.Fatalf("economy -> %q, want light", got)
	}
}

func TestPolicyOfIsStablePerPreset(t *testing.T) {
	light := PolicyOf(Light)
	if light.CapabilityPolicy.SemanticRouterAllowed {
		t.Fatal("light must not enable semantic capability router")
	}
	if light.ReviewPolicy.MediumRisk != ReviewNone {
		t.Fatal("light medium risk must not force independent review")
	}
	if light.ReviewForRisk(2, true) != ReviewForcedSecurity {
		t.Fatal("light high-risk security must elevate to forced security review")
	}

	balanced := PolicyOf(Balanced)
	if balanced.ReviewPolicy.MediumRisk != ReviewConditional {
		t.Fatal("balanced medium risk should be conditional review")
	}
	if !balanced.CapabilityPolicy.SemanticRouterAllowed {
		t.Fatal("balanced should allow semantic router when needed")
	}

	delivery := PolicyOf(Delivery)
	if !delivery.PlannerPolicy.RequireAtomicContract {
		t.Fatal("delivery must require atomic contracts on direct runs")
	}
	if delivery.ReviewPolicy.MediumRisk != ReviewForced {
		t.Fatal("delivery medium risk must force independent review")
	}
	if delivery.ReviewPolicy.LowRisk != ReviewNone {
		t.Fatal("delivery low risk must not spawn independent reviewer")
	}
	if delivery.VerificationPolicy.Level != VerifyFull {
		t.Fatal("delivery must require full verification")
	}
}

func TestIsValid(t *testing.T) {
	if !IsValid("light") || !IsValid("balanced") || !IsValid("delivery") {
		t.Fatal("canonical names must be valid")
	}
	if IsValid("economy") || IsValid("full") || IsValid("") {
		t.Fatal("legacy aliases must not pass IsValid")
	}
}
