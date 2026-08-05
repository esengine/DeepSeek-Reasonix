package config

import (
	"os"
	"testing"
)

// TestMCPMetaToolEnabledResolution verifies the config/env priority matrix for
// the run_mcp meta-tool dispatcher. The env var REASONIX_MCP_META_TOOL must
// override the [tools] meta_tool config when set, and the config value wins
// when the env var is unset. Nil config (the default) is off.
func TestMCPMetaToolEnabledResolution(t *testing.T) {
	saveAndClearEnv(t)

	trueVal, falseVal := true, false

	cases := []struct {
		name    string
		envSet  string // "" means unset, any other value means Setenv(name, envSet)
		envVal  string
		cfg     *bool
		want    bool
	}{
		// Config unset (nil), env unset → off (default, preserves legacy behavior).
		{"nil config, env unset", "unset", "", nil, false},

		// Config explicit, env unset → config wins.
		{"config true, env unset", "unset", "", &trueVal, true},
		{"config false, env unset", "unset", "", &falseVal, false},

		// Env overrides config in both directions.
		{"env 1 overrides config false", "set", "1", &falseVal, true},
		{"env true overrides config false", "set", "true", &falseVal, true},
		{"env on overrides config false", "set", "on", &falseVal, true},
		{"env yes overrides config false", "set", "yes", &falseVal, true},
		{"env 0 overrides config true", "set", "0", &trueVal, false},
		{"env false overrides config true", "set", "false", &trueVal, false},
		{"env off overrides config true", "set", "off", &trueVal, false},
		{"env no overrides config true", "set", "no", &trueVal, false},

		// Env set, config nil.
		{"env 1, nil config", "set", "1", nil, true},
		{"env 0, nil config", "set", "0", nil, false},

		// Env empty string = unset → config wins. An empty env var is treated
		// as "not a recognized spelling" so it falls through to config.
		{"env empty string, config true", "set", "", &trueVal, true},
		{"env empty string, config false", "set", "", &falseVal, false},
		{"env empty string, nil config", "set", "", nil, false},

		// Case insensitivity.
		{"env TRUE uppercase", "set", "TRUE", &falseVal, true},
		{"env On mixed case", "set", "On", &falseVal, true},
		{"env OFF uppercase", "set", "OFF", &trueVal, false},

		// Unrecognized spellings don't override config.
		{"env garbage, config true", "set", "maybe", &trueVal, true},
		{"env garbage, nil config", "set", "maybe", nil, false},

		// Whitespace is trimmed.
		{"env with spaces ' 1 '", "set", "  1  ", &falseVal, true},
		{"env with spaces ' off '", "set", "  off ", &trueVal, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet == "unset" {
				os.Unsetenv("REASONIX_MCP_META_TOOL")
			} else {
				os.Setenv("REASONIX_MCP_META_TOOL", tc.envVal)
			}
			cfg := &Config{}
			if tc.cfg != nil {
				v := *tc.cfg
				cfg.Tools.MetaTool = &v
			}
			got := cfg.MCPMetaToolEnabled()
			if got != tc.want {
				t.Errorf("MCPMetaToolEnabled() = %v, want %v (env=%q, cfg=%v)",
					got, tc.want, tc.envVal, tc.cfg)
			}
		})
	}
}

// TestMCPMetaToolEnabledNilReceiver ensures a nil Config doesn't panic.
func TestMCPMetaToolEnabledNilReceiver(t *testing.T) {
	saveAndClearEnv(t)
	os.Unsetenv("REASONIX_MCP_META_TOOL")
	var cfg *Config
	if cfg.MCPMetaToolEnabled() {
		t.Error("nil Config should report meta-tool disabled")
	}
	os.Setenv("REASONIX_MCP_META_TOOL", "1")
	if !cfg.MCPMetaToolEnabled() {
		t.Error("nil Config with env override should report meta-tool enabled")
	}
}

// TestMCPMetaToolEnvOverrideHelper directly exercises the env parser so the
// recognized-spelling contract is documented by example.
func TestMCPMetaToolEnvOverrideHelper(t *testing.T) {
	saveAndClearEnv(t)

	cases := []struct {
		env  string
		val  bool
		ok   bool
	}{
		{"", false, false},
		{"1", true, true},
		{"true", true, true},
		{"yes", true, true},
		{"on", true, true},
		{"0", false, true},
		{"false", false, true},
		{"no", false, true},
		{"off", false, true},
		{"maybe", false, false},
		{"TRUE", true, true},
		{"  on  ", true, true},
	}
	for _, tc := range cases {
		t.Run("env="+tc.env, func(t *testing.T) {
			os.Setenv("REASONIX_MCP_META_TOOL", tc.env)
			val, ok := mcpMetaToolEnvOverride()
			if val != tc.val || ok != tc.ok {
				t.Errorf("mcpMetaToolEnvOverride() = (%v, %v), want (%v, %v)",
					val, ok, tc.val, tc.ok)
			}
		})
	}
}

// saveAndClearEnv clears the env var for the test and restores it on cleanup.
func saveAndClearEnv(t *testing.T) {
	t.Helper()
	orig, hadOrig := os.LookupEnv("REASONIX_MCP_META_TOOL")
	os.Unsetenv("REASONIX_MCP_META_TOOL")
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("REASONIX_MCP_META_TOOL", orig)
		} else {
			os.Unsetenv("REASONIX_MCP_META_TOOL")
		}
	})
}
