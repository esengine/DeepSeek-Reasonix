package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveConfigTagNamesTheShadowingProjectFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default_model = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	t.Chdir(work)

	if got := activeConfigTag(); !strings.HasSuffix(got, "config.toml") {
		t.Fatalf("user-global config should be reported, got %q", got)
	}

	local := filepath.Join(work, "reasonix.toml")
	if err := os.WriteFile(local, []byte("default_model = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := activeConfigTag(); !strings.HasSuffix(got, "reasonix.toml") {
		t.Fatalf("project config outranks the global one and must be reported, got %q", got)
	}
}

func TestActiveConfigTagWithoutAnyConfigFile(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	if got := activeConfigTag(); !strings.Contains(got, "defaults") {
		t.Fatalf("no config file must be stated explicitly, got %q", got)
	}
}
