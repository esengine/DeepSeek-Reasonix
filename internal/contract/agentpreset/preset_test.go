package agentpreset

import "testing"

func TestNormalizeLegacyAndUnknown(t *testing.T) {
	cases := map[string]AgentPreset{
		"":           Balanced,
		"full":       Balanced,
		"balanced":   Balanced,
		"BALANCED":   Balanced,
		"economy":    Balanced,
		"eco":        Balanced,
		"light":      Balanced,
		"lite":       Balanced,
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
	// The retired setting resolves rather than failing: an old config that
	// still says economy gets the default, not an error.
	if got := FromLegacyTokenMode("economy"); got != Balanced {
		t.Fatalf("economy -> %q, want balanced", got)
	}
}

func TestPolicyOfIsStablePerPreset(t *testing.T) {
	if got := PolicyOf(Balanced).VerificationPolicy.Level; got != VerifyTargeted {
		t.Fatalf("balanced verification = %v, want targeted", got)
	}
	if got := PolicyOf(Delivery).VerificationPolicy.Level; got != VerifyFull {
		t.Fatal("delivery must require full verification")
	}
	// A legacy alias resolves to the default's policy, never to its own.
	if PolicyOf("light") != PolicyOf(Balanced) {
		t.Fatal("retired alias must answer the balanced policy")
	}
}

func TestIsValid(t *testing.T) {
	if !IsValid("balanced") || !IsValid("delivery") {
		t.Fatal("canonical names must be valid")
	}
	// "light" joins the aliases: Normalize still answers it, but it is no
	// longer a name anything should write back out.
	if IsValid("light") || IsValid("economy") || IsValid("full") || IsValid("") {
		t.Fatal("legacy aliases must not pass IsValid")
	}
}
