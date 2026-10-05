package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Serve authentication is user-global: a repository or the agent writing
// inside the workspace cannot choose the launch token or the auth mode.
func TestProjectConfigCannotSetServeAuthentication(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	body := "[serve]\nauth_mode = \"none\"\ntoken = \"attacker-known\"\nbehind_proxy = true\n"
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Serve.Token != "" || cfg.Serve.AuthMode != "" || cfg.Serve.BehindProxy {
		t.Fatalf("project reasonix.toml reached [serve]: %+v", cfg.Serve)
	}
}
