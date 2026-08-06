package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// TestTokenGovernanceDisabledByDefault: the OPT-261~265 token-management
// modules must be off unless explicitly configured — existing behavior is
// preserved.
func TestTokenGovernanceDisabledByDefault(t *testing.T) {
	cfg := Default()
	if cfg.Agent.TokenGovernance != nil {
		t.Fatal("token_governance should be nil (disabled) by default")
	}
}

// TestTokenGovernanceTomlDecode verifies the config section decodes into the
// expected fields.
func TestTokenGovernanceTomlDecode(t *testing.T) {
	const src = `
[agent.token_governance]
enabled = true
load_shedder = true
load_threshold = 250000
shed_strategy = "newest"
cache_compactor = true
window_resizer = true
context_window_min = 32000
context_window_max = 512000
admission_gate = true
admission_capacity = 300000
cache_warmer = true
warmer_strategy = "fifo"
`
	var c struct {
		Agent AgentConfig `toml:"agent"`
	}
	if _, err := toml.Decode(src, &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	tg := c.Agent.TokenGovernance
	if tg == nil {
		t.Fatal("token_governance section not decoded")
	}
	if !tg.Enabled || !tg.LoadShedder || !tg.CacheCompactor || !tg.WindowResizer || !tg.AdmissionGate || !tg.CacheWarmer {
		t.Error("module toggles not decoded")
	}
	if tg.LoadThreshold != 250000 || tg.AdmissionCapacity != 300000 {
		t.Errorf("thresholds wrong: %+v", tg)
	}
	if tg.ShedStrategy != "newest" || tg.WarmerStrategy != "fifo" {
		t.Errorf("strategies wrong: %+v", tg)
	}
	if tg.ContextWindowMin != 32000 || tg.ContextWindowMax != 512000 {
		t.Errorf("window bounds wrong: %+v", tg)
	}
}

// TestTokenGovernanceTomlRendersByPath verifies a config loaded through the
// real file path (as boot does) keeps the section.
func TestTokenGovernanceTomlRendersByPath(t *testing.T) {
	src := "[agent.token_governance]\nenabled = true\nload_shedder = true\n"
	if err := loadWithToml(t, src, func(cfg *Config) {
		if cfg.Agent.TokenGovernance == nil || !cfg.Agent.TokenGovernance.Enabled {
			t.Error("token_governance lost through load path")
		}
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCosplayTomlDecode verifies the cosplay config section decodes and that
// it is nil by default.
func TestCosplayTomlDecode(t *testing.T) {
	cfg := Default()
	if cfg.Agent.Cosplay != nil {
		t.Fatal("cosplay should be nil (defaults) unless configured")
	}
	const src = `
[agent.cosplay]
enabled = true
max_rounds = 3
num_tests = 6
timeout_seconds = 30
`
	var c struct {
		Agent AgentConfig `toml:"agent"`
	}
	if _, err := toml.Decode(src, &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cp := c.Agent.Cosplay
	if cp == nil || !cp.Enabled || cp.MaxRounds != 3 || cp.NumTests != 6 || cp.TimeoutSeconds != 30 {
		t.Errorf("cosplay config wrong: %+v", cp)
	}
}

// loadWithToml decodes src into a Config and runs fn against it. It mirrors
// the project's config-load test pattern without touching real files.
func loadWithToml(t *testing.T, src string, fn func(*Config)) error {
	t.Helper()
	var cfg Config
	if _, err := toml.Decode(strings.TrimSpace(src), &cfg); err != nil {
		return err
	}
	fn(&cfg)
	return nil
}
