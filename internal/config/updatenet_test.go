package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUpdateNetworkConfigReadsUserConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	userPath := filepath.Join(home, "config.toml")
	body := "[desktop]\nupdate_channel = \"preview\"\n\n[network]\nproxy_mode = \"custom\"\nproxy_url = \"socks5://127.0.0.1:7890\"\n"
	if err := os.WriteFile(userPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// A project config with an unparseable plugin path must be irrelevant.
	root := t.TempDir()
	project := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(project, []byte("[[plugins]]\ncommand = \"D:\\开发\\broken.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUpdateNetworkConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DesktopUpdateChannel() != "stable" {
		t.Errorf("update channel = %q, want official stable channel", cfg.DesktopUpdateChannel())
	}
	if cfg.NetworkProxyMode() != "custom" {
		t.Errorf("proxy mode = %q, want custom", cfg.NetworkProxyMode())
	}
	if cfg.Network.ProxyURL != "socks5://127.0.0.1:7890" {
		t.Errorf("proxy url = %q", cfg.Network.ProxyURL)
	}
}

func TestLoadUpdateNetworkConfigFallsBackToLastKnownGood(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	userPath := filepath.Join(home, "config.toml")
	// The live config is damaged (invalid escape).
	if err := os.WriteFile(userPath, []byte("command = \"D:\\开发\\x.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The last-known-good snapshot still carries the update settings.
	lkg := filepath.Join(home, "repair", "config.toml.last-known-good")
	if err := os.MkdirAll(filepath.Dir(lkg), 0o755); err != nil {
		t.Fatal(err)
	}
	lkgBody := "[cli]\nupdate_channel = \"preview\"\n\n[network]\nproxy_url = \"http://proxy.example:8080\"\n"
	if err := os.WriteFile(lkg, []byte(lkgBody), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUpdateNetworkConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CLIUpdateChannel() != "stable" {
		t.Errorf("update channel = %q, want legacy snapshot normalized to stable", cfg.CLIUpdateChannel())
	}
	if cfg.Network.ProxyURL != "http://proxy.example:8080" {
		t.Errorf("proxy url = %q, want snapshot value", cfg.Network.ProxyURL)
	}
}

func TestLoadUpdateNetworkConfigEnvDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	// No config at all: env proxy + defaults must still work.
	cfg, err := LoadUpdateNetworkConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NetworkProxyMode() != "auto" {
		t.Errorf("proxy mode = %q, want auto (env-driven)", cfg.NetworkProxyMode())
	}
	if cfg.DesktopUpdateChannel() != "stable" {
		t.Errorf("update channel = %q, want stable default", cfg.DesktopUpdateChannel())
	}
}
