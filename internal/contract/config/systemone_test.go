package config

import "testing"

func TestSystemOneAPIKeyUsesConfiguredEnvironmentName(t *testing.T) {
	t.Setenv("REASONIX_TYPESAFE_TEST_KEY", "secret")
	cfg := Config{Tools: ToolsConfig{SystemOne: SystemOneConfig{APIKeyEnv: "REASONIX_TYPESAFE_TEST_KEY"}}}
	if got := cfg.SystemOneAPIKey(); got != "secret" {
		t.Fatalf("SystemOneAPIKey() = %q", got)
	}
}
