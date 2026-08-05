package config

import (
	"os"
	"testing"
)

// TestLongHorizonDisabledByDefault verifies that long_horizon mode is off
// when neither config nor env var is set — preserving existing behavior.
func TestLongHorizonDisabledByDefault(t *testing.T) {
	os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	if cfg.LongHorizonEnabled() {
		t.Fatal("long_horizon should be disabled by default")
	}
	// Ratios should stay at standard defaults
	if cfg.Agent.SoftCompactRatio != 0.5 {
		t.Errorf("SoftCompactRatio = %v, want 0.5 (standard default)", cfg.Agent.SoftCompactRatio)
	}
	if cfg.Agent.ToolResultSnipRatio != 0.6 {
		t.Errorf("ToolResultSnipRatio = %v, want 0.6 (standard default)", cfg.Agent.ToolResultSnipRatio)
	}
	if cfg.Agent.VerificationInterval != 0 {
		t.Errorf("VerificationInterval = %v, want 0 (disabled)", cfg.Agent.VerificationInterval)
	}
}

// TestLongHorizonConfigEnabled verifies that [agent] long_horizon = true
// adjusts compaction ratios and sets verification interval.
func TestLongHorizonConfigEnabled(t *testing.T) {
	os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	enabled := true
	cfg.Agent.LongHorizon = &enabled
	normalizeLongHorizon(cfg)

	if !cfg.LongHorizonEnabled() {
		t.Fatal("LongHorizonEnabled() should be true after config sets it")
	}
	if cfg.Agent.SoftCompactRatio != 0.4 {
		t.Errorf("SoftCompactRatio = %v, want 0.4 (long-horizon adjusted)", cfg.Agent.SoftCompactRatio)
	}
	if cfg.Agent.ToolResultSnipRatio != 0.5 {
		t.Errorf("ToolResultSnipRatio = %v, want 0.5 (long-horizon adjusted)", cfg.Agent.ToolResultSnipRatio)
	}
	if cfg.Agent.VerificationInterval != 50 {
		t.Errorf("VerificationInterval = %v, want 50 (long-horizon default)", cfg.Agent.VerificationInterval)
	}
}

// TestLongHorizonPreservesExplicitRatios verifies that user-set ratios
// survive normalization — only standard defaults (0.5/0.6) are upgraded.
func TestLongHorizonPreservesExplicitRatios(t *testing.T) {
	os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	enabled := true
	cfg.Agent.LongHorizon = &enabled
	cfg.Agent.SoftCompactRatio = 0.35  // user override — should survive
	cfg.Agent.ToolResultSnipRatio = 0.42 // user override — should survive
	cfg.Agent.VerificationInterval = 30  // user override — should survive
	normalizeLongHorizon(cfg)

	if cfg.Agent.SoftCompactRatio != 0.35 {
		t.Errorf("SoftCompactRatio = %v, want 0.35 (user override preserved)", cfg.Agent.SoftCompactRatio)
	}
	if cfg.Agent.ToolResultSnipRatio != 0.42 {
		t.Errorf("ToolResultSnipRatio = %v, want 0.42 (user override preserved)", cfg.Agent.ToolResultSnipRatio)
	}
	if cfg.Agent.VerificationInterval != 30 {
		t.Errorf("VerificationInterval = %v, want 30 (user override preserved)", cfg.Agent.VerificationInterval)
	}
}

// TestLongHorizonEnvOverrideOn verifies REASONIX_LONG_HORIZON=1 enables
// the mode even when config doesn't set it.
func TestLongHorizonEnvOverrideOn(t *testing.T) {
	os.Setenv("REASONIX_LONG_HORIZON", "1")
	defer os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	// Config doesn't set LongHorizon, but env should enable it
	if !cfg.LongHorizonEnabled() {
		t.Fatal("LongHorizonEnabled() should be true when env var is 1")
	}
	normalizeLongHorizon(cfg)
	if cfg.Agent.SoftCompactRatio != 0.4 {
		t.Errorf("SoftCompactRatio = %v, want 0.4 (env-enabled long-horizon)", cfg.Agent.SoftCompactRatio)
	}
}

// TestLongHorizonEnvOverrideOff verifies REASONIX_LONG_HORIZON=0 disables
// the mode even when config sets long_horizon = true.
func TestLongHorizonEnvOverrideOff(t *testing.T) {
	os.Setenv("REASONIX_LONG_HORIZON", "0")
	defer os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	enabled := true
	cfg.Agent.LongHorizon = &enabled
	if cfg.LongHorizonEnabled() {
		t.Fatal("LongHorizonEnabled() should be false when env var is 0 (overrides config true)")
	}
	normalizeLongHorizon(cfg)
	// Should NOT adjust ratios because env override disabled it
	if cfg.Agent.SoftCompactRatio != 0.5 {
		t.Errorf("SoftCompactRatio = %v, want 0.5 (env-disabled, standard default)", cfg.Agent.SoftCompactRatio)
	}
}

// TestLongHorizonEnvGarbageIgnored verifies that unrecognized env values
// fall through to config, not silently enabling/disabling.
func TestLongHorizonEnvGarbageIgnored(t *testing.T) {
	os.Setenv("REASONIX_LONG_HORIZON", "maybe")
	defer os.Unsetenv("REASONIX_LONG_HORIZON")
	cfg := Default()
	// Garbage value should not override — falls through to config (nil = false)
	if cfg.LongHorizonEnabled() {
		t.Fatal("LongHorizonEnabled() should be false for unrecognized env value")
	}
}

// TestLongHorizonCompactionRatiosMatrix is a table-driven test covering
// all combinations of config value × env var.
func TestLongHorizonCompactionRatiosMatrix(t *testing.T) {
	cases := []struct {
		name     string
		config   *bool // nil = unset, &true = true, &false = false
		env      string
		wantOn   bool
		wantSoft float64
		wantSnip float64
		wantVI   int
	}{
		{"nil/no-env", nil, "", false, 0.5, 0.6, 0},
		{"nil/env-on", nil, "1", true, 0.4, 0.5, 50},
		{"nil/env-off", nil, "0", false, 0.5, 0.6, 0},
		{"true/no-env", &[]bool{true}[0], "", true, 0.4, 0.5, 50},
		{"true/env-on", &[]bool{true}[0], "1", true, 0.4, 0.5, 50},
		{"true/env-off", &[]bool{true}[0], "0", false, 0.5, 0.6, 0},
		{"false/no-env", &[]bool{false}[0], "", false, 0.5, 0.6, 0},
		{"false/env-on", &[]bool{false}[0], "1", true, 0.4, 0.5, 50},
		{"false/env-off", &[]bool{false}[0], "0", false, 0.5, 0.6, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env == "" {
				os.Unsetenv("REASONIX_LONG_HORIZON")
			} else {
				os.Setenv("REASONIX_LONG_HORIZON", tc.env)
			}
			defer os.Unsetenv("REASONIX_LONG_HORIZON")
			cfg := Default()
			if tc.config != nil {
				v := *tc.config
				cfg.Agent.LongHorizon = &v
			}
			normalizeLongHorizon(cfg)
			if got := cfg.LongHorizonEnabled(); got != tc.wantOn {
				t.Errorf("LongHorizonEnabled() = %v, want %v", got, tc.wantOn)
			}
			if cfg.Agent.SoftCompactRatio != tc.wantSoft {
				t.Errorf("SoftCompactRatio = %v, want %v", cfg.Agent.SoftCompactRatio, tc.wantSoft)
			}
			if cfg.Agent.ToolResultSnipRatio != tc.wantSnip {
				t.Errorf("ToolResultSnipRatio = %v, want %v", cfg.Agent.ToolResultSnipRatio, tc.wantSnip)
			}
			if cfg.Agent.VerificationInterval != tc.wantVI {
				t.Errorf("VerificationInterval = %v, want %v", cfg.Agent.VerificationInterval, tc.wantVI)
			}
		})
	}
}
