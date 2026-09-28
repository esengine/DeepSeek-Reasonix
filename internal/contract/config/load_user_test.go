package config

import (
	"testing"

	"reasonix/internal/base/testenv"
)

func TestLoadUserConfigReadOnlyIgnoresTheWorkingDirectoryProject(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	writeProjectDefaultTestConfig(t, home, "config.toml", "[tools.search]\nengine = \"native\"\n")

	project := testenv.TempDir(t)
	writeProjectDefaultTestConfig(t, project, "reasonix.toml", "[tools.search]\nengine = \"rg\"\nrg_path = \"tools/rg\"\n\n[sandbox]\nbash = \"off\"\n")
	t.Chdir(project)

	merged, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if merged.Tools.Search.RgPath != "tools/rg" {
		t.Fatalf("precondition: merged load should read the project file, got %+v", merged.Tools.Search)
	}

	cfg, err := merged.Roots().LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Search.Engine != "native" || cfg.Tools.Search.RgPath != "" {
		t.Fatalf("user-only search = %+v, want the user's native engine", cfg.Tools.Search)
	}
	if cfg.Sandbox.Bash == "off" {
		t.Fatal("user-only config took the project's sandbox mode")
	}
}

func TestLoadUserConfigReadOnlyFallsBackOnABrokenFile(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	writeProjectDefaultTestConfig(t, home, "config.toml", "[tools.search\n")

	cfg, err := LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Search.RgPath != "" {
		t.Fatalf("broken user config should fall back to defaults, got %+v", cfg.Tools.Search)
	}
}

func TestProjectConfigCannotSetShellEnv(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	writeProjectDefaultTestConfig(t, home, "config.toml", "[tools.shell.env]\nCI = \"1\"\n")

	project := testenv.TempDir(t)
	writeProjectDefaultTestConfig(t, project, "reasonix.toml", "[tools.shell.env]\nPATH = \"/repo/bin\"\nGIT_SSH_COMMAND = \"ssh -i /repo/key\"\n")
	t.Chdir(project)

	merged, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if merged.Tools.Shell.Env["CI"] != "1" {
		t.Fatalf("user shell env lost: %v", merged.Tools.Shell.Env)
	}
	if _, ok := merged.Tools.Shell.Env["PATH"]; ok {
		t.Fatalf("project injected PATH into shell env: %v", merged.Tools.Shell.Env)
	}
	if _, ok := merged.Tools.Shell.Env["GIT_SSH_COMMAND"]; ok {
		t.Fatalf("project injected GIT_SSH_COMMAND into shell env: %v", merged.Tools.Shell.Env)
	}
}
