package config

import (
	"testing"
)

func TestLarkConfigDefaults(t *testing.T) {
	cfg := Default()

	if cfg.Lark.Enabled() {
		t.Error("Lark should be disabled by default (empty credentials)")
	}

	if got := cfg.Lark.ResolvedSessionTTL(); got != "1h" {
		t.Errorf("default session TTL = %q, want %q", got, "1h")
	}

	if got := cfg.Lark.ResolvedMaxSessions(); got != 0 {
		t.Errorf("default max sessions = %d, want 0 (unlimited)", got)
	}

	if got := cfg.Lark.ResolvedGroupPermission(); got != "read-only" {
		t.Errorf("default group permission = %q, want %q", got, "read-only")
	}

	if got := cfg.Lark.ResolvedDMPermission(); got != "interactive" {
		t.Errorf("default DM permission = %q, want %q", got, "interactive")
	}

	if got := cfg.Lark.ResolvedMaxResponseLength(); got != 8000 {
		t.Errorf("default max response length = %d, want 8000", got)
	}

	if got := cfg.Lark.ResolvedApprovalTimeout(); got != "5m" {
		t.Errorf("default approval timeout = %q, want %q", got, "5m")
	}
}

func TestLarkConfigEnabled(t *testing.T) {
	cfg := LarkConfig{}
	if cfg.Enabled() {
		t.Error("empty config should not be enabled")
	}

	cfg = LarkConfig{AppID: "cli_xxx"}
	if cfg.Enabled() {
		t.Error("config with only app_id should not be enabled")
	}

	cfg = LarkConfig{AppID: "cli_xxx", AppSecret: "secret"}
	if !cfg.Enabled() {
		t.Error("config with both app_id and app_secret should be enabled")
	}
}

func TestLarkConfigEnvResolution(t *testing.T) {
	t.Setenv("LARK_TEST_ID", "cli_from_env")
	t.Setenv("LARK_TEST_SECRET", "secret_from_env")

	cfg := LarkConfig{
		AppIDEnv:     "LARK_TEST_ID",
		AppSecretEnv: "LARK_TEST_SECRET",
	}
	if got := cfg.ResolvedAppID(); got != "cli_from_env" {
		t.Errorf("AppID from env = %q, want %q", got, "cli_from_env")
	}
	if got := cfg.ResolvedAppSecret(); got != "secret_from_env" {
		t.Errorf("AppSecret from env = %q, want %q", got, "secret_from_env")
	}
	if !cfg.Enabled() {
		t.Error("should be enabled when env vars are set")
	}
}

func TestLarkConfigEnvFallback(t *testing.T) {
	cfg := LarkConfig{
		AppIDEnv:     "LARK_TEST_MISSING",
		AppSecretEnv: "LARK_TEST_MISSING",
		AppID:        "cli_fallback",
		AppSecret:    "secret_fallback",
	}
	if got := cfg.ResolvedAppID(); got != "cli_fallback" {
		t.Errorf("AppID fallback = %q, want %q", got, "cli_fallback")
	}
	if got := cfg.ResolvedAppSecret(); got != "secret_fallback" {
		t.Errorf("AppSecret fallback = %q, want %q", got, "secret_fallback")
	}
}

func TestLarkConfigEnvVarExpansion(t *testing.T) {
	t.Setenv("MY_ID", "cli_expanded")
	cfg := LarkConfig{
		AppID:     "${MY_ID}",
		AppSecret: "direct_secret",
	}
	if got := cfg.ResolvedAppID(); got != "cli_expanded" {
		t.Errorf("AppID expanded = %q, want %q", got, "cli_expanded")
	}
	if got := cfg.ResolvedAppSecret(); got != "direct_secret" {
		t.Errorf("AppSecret direct = %q, want %q", got, "direct_secret")
	}
}

func TestLarkConfigCustomValues(t *testing.T) {
	cfg := LarkConfig{
		AppID:             "cli_abc",
		AppSecret:         "secret",
		SessionTTL:        "30m",
		MaxSessions:       20,
		GroupPermission:   "interactive",
		DMPermission:      "bypass",
		ShowReasoning:     true,
		MaxResponseLength: 4000,
		ApprovalTimeout:   "10m",
	}

	if got := cfg.ResolvedSessionTTL(); got != "30m" {
		t.Errorf("session TTL = %q, want %q", got, "30m")
	}

	if got := cfg.ResolvedMaxSessions(); got != 20 {
		t.Errorf("max sessions = %d, want 20", got)
	}

	if got := cfg.ResolvedGroupPermission(); got != "interactive" {
		t.Errorf("group permission = %q, want %q", got, "interactive")
	}

	if got := cfg.ResolvedDMPermission(); got != "bypass" {
		t.Errorf("DM permission = %q, want %q", got, "bypass")
	}

	if got := cfg.ResolvedMaxResponseLength(); got != 4000 {
		t.Errorf("max response length = %d, want 4000", got)
	}

	if got := cfg.ResolvedApprovalTimeout(); got != "10m" {
		t.Errorf("approval timeout = %q, want %q", got, "10m")
	}
}

func TestLarkConfigInvalidPermission(t *testing.T) {
	cfg := LarkConfig{
		GroupPermission: "invalid",
		DMPermission:    "",
	}

	if got := cfg.ResolvedGroupPermission(); got != "read-only" {
		t.Errorf("invalid group permission should fall back to 'read-only', got %q", got)
	}

	if got := cfg.ResolvedDMPermission(); got != "interactive" {
		t.Errorf("empty DM permission should fall back to 'interactive', got %q", got)
	}
}

func TestLarkConfigNegativeMaxSessions(t *testing.T) {
	cfg := LarkConfig{MaxSessions: -1}
	if got := cfg.ResolvedMaxSessions(); got != 0 {
		t.Errorf("negative max sessions = %d, want 0", got)
	}
}
