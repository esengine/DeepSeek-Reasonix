package config

import (
	"context"
	"os"
	"testing"

	"reasonix/internal/testenv"
)

func TestMain(m *testing.M) {
	if os.Getenv("REASONIX_CONFIG_LOCK_HELPER") == "1" {
		os.Exit(m.Run())
	}
	// RunWithIsolatedUserState redirects every path-shaped location, but the OS
	// keyring has no environment variable in front of it, so legacy migration
	// would read the credentials of whoever is running the tests. Cases that
	// exercise the lookup substitute their own.
	legacyKeyringProbeLookup = func(context.Context, string) legacyKeyringOutcome {
		return legacyKeyringOutcome{Status: legacyKeyringAbsent}
	}
	testenv.RunWithIsolatedUserState(m)
}

// Guards the isolation above: a clean CI runner has an empty keyring and would
// stay green either way, so the substitution itself is what gets asserted.
func TestKeyringLookupStaysOutOfTheRealStore(t *testing.T) {
	for _, key := range []string{"DEEPSEEK_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		o := legacyKeyringProbeLookup(context.Background(), key)
		if o.Status != legacyKeyringAbsent || o.Value != "" {
			t.Fatalf("%s resolved through the real keyring in tests: %+v", key, o)
		}
	}
}
