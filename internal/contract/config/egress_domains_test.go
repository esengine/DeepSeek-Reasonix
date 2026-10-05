package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func loadWithSandbox(t *testing.T, global, project string) *Config {
	t.Helper()
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[sandbox]\n"+global), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[sandbox]\n"+project), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// A repo may narrow open network to a list, but never replace the user's list:
// that would let a clone widen what the user limited.
func TestRepoEgressDomainsOnlyNarrow(t *testing.T) {
	cfg := loadWithSandbox(t, `allowed_domains = ["github.com"]`+"\n"+`denied_domains = ["gist.github.com"]`,
		`allowed_domains = ["*.com"]`+"\n"+`denied_domains = ["evil.example"]`)
	if !slices.Equal(cfg.Sandbox.AllowedDomains, []string{"github.com"}) {
		t.Fatalf("allowed = %v, want the user's list", cfg.Sandbox.AllowedDomains)
	}
	if !slices.Equal(cfg.Sandbox.DeniedDomains, []string{"gist.github.com", "evil.example"}) {
		t.Fatalf("denied = %v, want both lists", cfg.Sandbox.DeniedDomains)
	}

	cfg = loadWithSandbox(t, "", `allowed_domains = ["pypi.org"]`)
	if !slices.Equal(cfg.Sandbox.AllowedDomains, []string{"pypi.org"}) {
		t.Fatalf("allowed = %v, want the repo's list where the user set none", cfg.Sandbox.AllowedDomains)
	}
}
