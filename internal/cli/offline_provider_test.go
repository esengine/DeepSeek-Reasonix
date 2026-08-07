package cli

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

// pinProviderOffline points the isolated home at a provider that can never be
// reached, so a run under test fails at setup instead of dialling a real API.
// Without it the default config targets api.deepseek.com and the assertion
// silently depends on how that host rejects an unauthenticated call.
func pinProviderOffline(t *testing.T) {
	t.Helper()
	path := config.UserConfigPath()
	if path == "" {
		t.Fatal("UserConfigPath() is empty under an isolated home")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "offline-test/model-a"

[[providers]]
name = "offline-test"
kind = "openai"
base_url = "https://reasonix-tests.invalid/v1"
models = ["model-a"]
default = "model-a"
api_key_env = "REASONIX_TEST_ABSENT_KEY"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
