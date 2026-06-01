package config

import "testing"

// TestProviderConfigured verifies Configured tracks whether the api_key_env
// resolves to a non-empty value — the same key check Validate enforces at build
// time, so model pickers can filter on it.
func TestProviderConfigured(t *testing.T) {
	t.Setenv("REASONIX_TEST_KEY", "secret")
	t.Setenv("REASONIX_TEST_EMPTY", "")

	cases := []struct {
		name string
		p    ProviderEntry
		want bool
	}{
		{"key set", ProviderEntry{APIKeyEnv: "REASONIX_TEST_KEY"}, true},
		{"key env empty", ProviderEntry{APIKeyEnv: "REASONIX_TEST_EMPTY"}, false},
		{"key env unset", ProviderEntry{APIKeyEnv: "REASONIX_TEST_MISSING"}, false},
		{"no api_key_env", ProviderEntry{}, false},
	}
	for _, c := range cases {
		if got := c.p.Configured(); got != c.want {
			t.Errorf("%s: Configured() = %v, want %v", c.name, got, c.want)
		}
	}
}
