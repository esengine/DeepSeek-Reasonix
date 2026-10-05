package config

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func loadWithCommandMode(t *testing.T, user, project string) *Config {
	t.Helper()
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if user != "" {
		if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[ui]\ncommandmode = \""+user+"\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[ui]\ncommandmode = \""+project+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestProjectCommandModeIsIgnored(t *testing.T) {
	if loadWithCommandMode(t, "", "vi").UICommandMode() {
		t.Fatal("a project-only commandmode enabled vi mode; the user's key bindings must win")
	}
}

func TestUserCommandModeWinsOverProject(t *testing.T) {
	if !loadWithCommandMode(t, "vi", "").UICommandMode() {
		t.Fatal("the user's commandmode = vi did not survive the project merge")
	}
}
