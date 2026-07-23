package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeModeIgnoresBrokenUserAndProjectConfig(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	for _, path := range []string{filepath.Join(home, "config.toml"), filepath.Join(root, "reasonix.toml")} {
		if err := os.WriteFile(path, []byte("[broken\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("safe mode load: %v", err)
	}
	if !cfg.SafeMode() || len(cfg.Plugins) != 0 || cfg.Bot.Enabled || cfg.DesktopCheckUpdates() {
		t.Fatalf("unsafe recovery config: %+v", cfg)
	}
}

func TestRecoveryDefaultsDoNotReadOrRewriteMalformedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	bad := []byte("[broken\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadRecoveryDefaultsForRoot(t.TempDir())
	if cfg == nil || !cfg.SafeMode() || len(cfg.Providers) == 0 {
		t.Fatalf("recovery defaults = %+v", cfg)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(bad) {
		t.Fatalf("malformed config was rewritten: %q", got)
	}
}

func TestSafeModeForcesReportingOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	optIn := "[desktop]\ntelemetry = true\nmetrics = true\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(optIn), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatalf("safe mode load: %v", err)
	}
	if cfg.DesktopTelemetry() || cfg.DesktopMetrics() {
		t.Fatalf("safe mode reporting on: telemetry=%v metrics=%v, want both false",
			cfg.DesktopTelemetry(), cfg.DesktopMetrics())
	}
}
