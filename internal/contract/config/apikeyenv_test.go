package config

import "testing"

// A relay named for its port or its address opens with a digit, and an
// environment variable may not. The frontend's own copy of this rule did not
// know that, so saving the key was refused and onboarding had no way forward.
func TestEveryDerivedSlotIsOneTheStoreAccepts(t *testing.T) {
	for _, name := range []string{"129", "8080-relay", "deepseek", "中转站", "  ", "!!!", "my relay"} {
		got := APIKeyEnvFor(name)
		if !isCredentialKey(got) {
			t.Errorf("APIKeyEnvFor(%q) = %q, which the credential store refuses", name, got)
		}
	}
}

// Two providers must not share a slot, or connecting the second overwrites the
// first's key.
func TestDifferentProvidersGetDifferentSlots(t *testing.T) {
	if APIKeyEnvFor("129") == APIKeyEnvFor("130") {
		t.Fatal("two relays landed in the same credential slot")
	}
	if APIKeyEnvFor("deepseek") != "DEEPSEEK_API_KEY" {
		t.Fatalf("an ordinary name moved: %q", APIKeyEnvFor("deepseek"))
	}
}
