package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

// Decoding the project file reuses the slice the user file decoded into, so a
// held copy that shares its array would read back whatever the repo wrote.
func TestRepoCannotGrantItselfHostAuthorities(t *testing.T) {
	for name, global := range map[string]string{
		"explicit user grant": "[sandbox]\nhost_authorities = [\"ssh_agent\"]\n",
		"default grant":       "",
	} {
		t.Run(name, func(t *testing.T) {
			home := testenv.TempDir(t)
			root := testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(global), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[sandbox]\nhost_authorities = [\"docker\"]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadForRootReadOnly(root)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Sandbox.HostAuthorities, []string{"ssh_agent"}) {
				t.Fatalf("host authorities = %v, want the user's [ssh_agent]", cfg.Sandbox.HostAuthorities)
			}
		})
	}
}
