package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLITelemetryConfigDefaultsAndValidation(t *testing.T) {
	cfg := Default()
	if got := cfg.CLITelemetryMode(); got != "auto" {
		t.Fatalf("default mode = %q", got)
	}
	if cfg.CLITelemetryConfigured() {
		t.Fatal("default config should preserve the undecided telemetry state")
	}
	for _, mode := range []string{"auto", "on", "off"} {
		if err := cfg.SetCLITelemetryMode(mode); err != nil || cfg.CLITelemetryMode() != mode {
			t.Fatalf("SetCLITelemetryMode(%q) = %q, %v", mode, cfg.CLITelemetryMode(), err)
		}
		if !cfg.CLITelemetryConfigured() {
			t.Fatalf("SetCLITelemetryMode(%q) did not record an explicit decision", mode)
		}
	}
	if err := cfg.SetCLITelemetryMode("sometimes"); err == nil {
		t.Fatal("invalid telemetry mode accepted")
	}
}

func TestCLITelemetryIsUserGlobalAndSafeModeForcesOff(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[telemetry]\ncli_metrics = \"on\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[telemetry]\ncli_metrics = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CLITelemetryMode(); got != "on" {
		t.Fatalf("project overrode user telemetry: %q", got)
	}
	t.Setenv("REASONIX_SAFE_MODE", "1")
	safe, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := safe.CLITelemetryMode(); got != "off" {
		t.Fatalf("Safe Mode telemetry = %q", got)
	}
}

func TestTelemetryRenderingIsOmittedFromProjectConfig(t *testing.T) {
	cfg := Default()
	cfg.Telemetry.CLIMetrics = "on"
	user := RenderTOMLForScope(cfg, RenderScopeUser)
	project := RenderTOMLForScope(cfg, RenderScopeProject)
	if !strings.Contains(user, "[telemetry]") || !strings.Contains(user, `cli_metrics = "on"`) {
		t.Fatalf("user render missing telemetry:\n%s", user)
	}
	if strings.Contains(project, "[telemetry]") || strings.Contains(project, "cli_metrics") {
		t.Fatalf("project render contains user-global telemetry:\n%s", project)
	}
}

func TestTelemetryRenderingPreservesUndecidedState(t *testing.T) {
	cfg := Default()
	user := RenderTOMLForScope(cfg, RenderScopeUser)
	if strings.Contains(user, "[telemetry]") || strings.Contains(user, "cli_metrics") {
		t.Fatalf("undecided telemetry was materialized during render:\n%s", user)
	}

	for _, mode := range []string{"auto", "on", "off"} {
		t.Run(mode, func(t *testing.T) {
			explicit := Default()
			if err := explicit.SetCLITelemetryMode(mode); err != nil {
				t.Fatal(err)
			}
			rendered := RenderTOMLForScope(explicit, RenderScopeUser)
			if !strings.Contains(rendered, "[telemetry]") || !strings.Contains(rendered, `cli_metrics = "`+mode+`"`) {
				t.Fatalf("explicit telemetry mode did not render:\n%s", rendered)
			}
		})
	}
}
