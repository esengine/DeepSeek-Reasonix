package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

const shellSeedConfig = `config_version = 6
default_model = "deepseek-flash/deepseek-flash"

[[providers]]
name        = "deepseek-flash"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
models      = ["deepseek-v4-flash"]
default     = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`

func seedShellConfig(t *testing.T) string {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(shellSeedConfig), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return path
}

// The interpreter has to survive a relaunch — boot reads it out of the config
// file, so a choice that only reached the running process is a setting that
// silently reverts. The write rewrites the whole file, hence the provider check.
func TestSaveShellSettingsPersists(t *testing.T) {
	path := seedShellConfig(t)
	if err := (&Controller{}).SaveShellSettings("powershell", ""); err != nil {
		t.Fatalf("save: %v", err)
	}
	saved := config.LoadForEdit(path)
	if saved.Tools.Shell.Prefer != "powershell" {
		t.Fatalf("prefer = %q, want powershell", saved.Tools.Shell.Prefer)
	}
	if _, ok := saved.Provider("deepseek-flash"); !ok {
		written, _ := os.ReadFile(path)
		t.Fatalf("provider block lost by the shell write; file now:\n%s", written)
	}
	if saved.DefaultModel != "deepseek-flash/deepseek-flash" {
		t.Fatalf("default_model = %q, want it preserved", saved.DefaultModel)
	}
}

// A path that cannot run is refused where it was typed. Writing it would move
// the failure to every later command, with nothing on screen connecting the two.
func TestSaveShellSettingsRefusesUnusablePath(t *testing.T) {
	path := seedShellConfig(t)
	missing := filepath.Join(testenv.TempDir(t), "no-such-bash")
	if err := (&Controller{}).SaveShellSettings("bash", missing); err == nil {
		t.Fatal("saving a missing executable succeeded")
	}
	if saved := config.LoadForEdit(path); saved.Tools.Shell.Path != "" {
		t.Fatalf("refused path was written anyway: %q", saved.Tools.Shell.Path)
	}
}

// Auto is a real answer, not an empty one: it clears a pinned path so the next
// launch goes back to detection instead of holding a shell the user unpicked.
func TestSaveShellSettingsAutoClearsPinnedPath(t *testing.T) {
	path := seedShellConfig(t)
	c := &Controller{}
	// Pinning proves the interpreter runs before saving it, so the path has to be
	// one this host really has — /bin/sh only ever existed off Windows. Prefer is
	// what the option itself says to pass, rather than a guess at the mapping.
	opts := c.ShellSettings().Options
	if len(opts) == 0 {
		t.Skip("no interpreter detected on this host")
	}
	pin := opts[0]
	if err := c.SaveShellSettings(pin.Prefer, pin.Path); err != nil {
		t.Fatalf("pin %s at %s: %v", pin.Prefer, pin.Path, err)
	}
	if err := c.SaveShellSettings("auto", ""); err != nil {
		t.Fatalf("auto: %v", err)
	}
	saved := config.LoadForEdit(path)
	if saved.Tools.Shell.Prefer != "auto" || saved.Tools.Shell.Path != "" {
		t.Fatalf("shell = %+v, want auto with no path", saved.Tools.Shell)
	}
}

// What the pane offers has to be what the machine has: every option carries the
// path it was probed at, and the effective row has to be one of them rather than
// a fourth answer nobody can select.
func TestShellSettingsOffersOnlyInstalledInterpreters(t *testing.T) {
	seedShellConfig(t)
	s := (&Controller{}).ShellSettings()
	if len(s.Options) == 0 {
		t.Skip("no interpreter detected on this host")
	}
	for _, o := range s.Options {
		if o.Path == "" || o.Name == "" {
			t.Fatalf("option without identity: %+v", o)
		}
	}
	if s.Auto.Path != s.Options[0].Path {
		t.Fatalf("auto = %q, want the first offered %q", s.Auto.Path, s.Options[0].Path)
	}
	if s.Prefer != "auto" {
		t.Fatalf("prefer = %q, want auto from a config that sets no shell", s.Prefer)
	}
	if s.Effective.Path != s.Auto.Path {
		t.Fatalf("effective = %q, want auto's %q", s.Effective.Path, s.Auto.Path)
	}
}

// Git for Windows ships bin/bash.exe as a launcher for usr/bin/bash.exe; the
// pane offers one row per install, and a pinned path is the one kept.
func TestOneGitBashPerInstall(t *testing.T) {
	usr := ShellOption{Name: "git-bash", Path: `C:\Program Files\Git\usr\bin\bash.exe`}
	bin := ShellOption{Name: "git-bash", Path: `C:\Program Files\Git\bin\bash.exe`}
	other := ShellOption{Name: "git-bash", Path: `D:\tools\Git\bin\bash.exe`}
	pwsh := ShellOption{Name: "pwsh", Path: `C:\Program Files\PowerShell\7\pwsh.exe`}
	opts := []ShellOption{usr, bin, other, pwsh}

	got := oneGitBashPerInstall(opts, usr.Path)
	if len(got) != 3 || got[0] != usr || got[1] != other || got[2] != pwsh {
		t.Fatalf("auto keep: %+v", got)
	}
	got = oneGitBashPerInstall(opts, bin.Path)
	if len(got) != 3 || got[0] != bin {
		t.Fatalf("pinned launcher not kept: %+v", got)
	}
	if opts[0] != usr {
		t.Fatal("input slice was rewritten")
	}
}
