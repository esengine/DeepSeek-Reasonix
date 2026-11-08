package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestAndroidShellDefaultsSurviveUnrelatedSettingsAndAllowUserOverride(t *testing.T) {
	cfg := Default()
	cfg.Tools.Shell = defaultShell("android")
	if _, err := toml.Decode("language = 'zh'\n[tools.shell.env]\nEDITOR = 'vi'\n", cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Prefer != "bash" || cfg.Tools.Shell.Path != "/system/bin/sh" || cfg.Tools.Shell.Env["EDITOR"] != "vi" {
		t.Fatalf("Android shell = %+v", cfg.Tools.Shell)
	}
	if _, err := toml.Decode("[tools.shell]\nprefer = 'bash'\npath = '/data/local/bin/bash'\n", cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Path != "/data/local/bin/bash" {
		t.Fatalf("user shell override = %q", cfg.Tools.Shell.Path)
	}
	for _, platform := range []string{"windows", "linux", "darwin"} {
		if shell := defaultShell(platform); shell.Prefer != "" || shell.Path != "" {
			t.Fatalf("%s must retain shell discovery: %+v", platform, shell)
		}
	}
}
