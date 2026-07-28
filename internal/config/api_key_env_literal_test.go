package config

import "testing"

// TestLooksLikeLiteralAPIKeyValue guards the migration-hint heuristic for
// configs carried over from before 1.11, where api_key_env held the key value
// itself. The heuristic must catch real secret shapes and never misclassify a
// legitimate UPPER_SNAKE env-var name.
func TestLooksLikeLiteralAPIKeyValue(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"empty", "", false},
		{"short env var name", "OPENAI_API_KEY", false},
		{"short underscored name", "REASONIX_TEST_KEY_UNSET", false},
		{"lowercase short name (not a secret)", "my_key", false},
		{"sk- prefixed secret", "sk-proj-1234567890abcdefghij1234567890", true},
		{"AIza google key", "AIzaSyABCDEFG1234567890abcdefghij1234567890", true},
		{"anthropic prefix", "sk-ant-api03-1234567890abcdefghij1234567890", true},
		{"long mixed-case with dot", "akia1234567890.something/with-path-and-mixed-case-1234567890", true},
		{"long UPPER no lowercase (env var)", "A_VERY_LONG_UPPER_SNAKE_NAME_THAT_IS_NOT_A_SECRET_AT_ALL", false},
		{"digits only long", "1234567890123456789012345678901234567890", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &ProviderEntry{APIKeyEnv: tc.env}
			if got := e.LooksLikeLiteralAPIKeyValue(); got != tc.want {
				t.Fatalf("LooksLikeLiteralAPIKeyValue(%q) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

// TestSuggestAPIKeyEnvName is in package boot; this only covers the config-side
// predicate. Nil-safety check.
func TestLooksLikeLiteralAPIKeyValueNilSafe(t *testing.T) {
	var e *ProviderEntry
	if e.LooksLikeLiteralAPIKeyValue() {
		t.Fatal("nil entry must not look like a literal value")
	}
}
