package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
)

// TestDiffFormatterIsUserGlobal proves a repository-local reasonix.toml cannot run
// an external diff formatter on the user's machine.
func TestDiffFormatterIsUserGlobal(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[cli]\ndiff_formatter = \"cat\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[cli]\ndiff_formatter = \"rm -rf /\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CLI.DiffFormatter; got != "cat" {
		t.Fatalf("project diff_formatter leaked: got %q, want the user-global %q", got, "cat")
	}
}

// TestDiffFormatterRendering proves the user-global [cli] section renders when set
// and never appears in a project-scoped render.
func TestDiffFormatterRendering(t *testing.T) {
	cfg := Default()
	cfg.CLI.DiffFormatter = "delta --color-only --paging=never"

	user := RenderTOMLForScope(cfg, RenderScopeUser)
	if !strings.Contains(user, "[cli]") || !strings.Contains(user, `diff_formatter = "delta --color-only --paging=never"`) {
		t.Fatalf("user render missing diff_formatter:\n%s", user)
	}
	project := RenderTOMLForScope(cfg, RenderScopeProject)
	if strings.Contains(project, "[cli]") || strings.Contains(project, "diff_formatter") {
		t.Fatalf("project render contains user-global CLI config:\n%s", project)
	}
}

// TestDiffFormatterRoundTrips proves the rendered key reloads into the same field.
func TestDiffFormatterRoundTrips(t *testing.T) {
	cfg := Default()
	cfg.CLI.DiffFormatter = "delta --paging=never"

	var got Config
	if _, err := toml.Decode(RenderTOMLForScope(cfg, RenderScopeUser), &got); err != nil {
		t.Fatalf("rendered TOML does not parse: %v", err)
	}
	if got.CLI.DiffFormatter != cfg.CLI.DiffFormatter {
		t.Fatalf("round-trip diff_formatter = %q, want %q", got.CLI.DiffFormatter, cfg.CLI.DiffFormatter)
	}
}

// TestDiffFencesDefaultsOff proves fenced diff rendering is opt-in.
func TestDiffFencesDefaultsOff(t *testing.T) {
	if Default().CLI.DiffFences {
		t.Fatal("diff_fences is on by default")
	}
}

// TestDiffFencesIsUserGlobal proves a repository-local reasonix.toml cannot turn
// on diff-fence rendering, and that the user-global render emits it only when set.
func TestDiffFencesIsUserGlobal(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("[cli]\ndiff_fences = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("[cli]\ndiff_fences = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CLI.DiffFences {
		t.Fatal("project diff_fences leaked over the user-global value")
	}
	user := RenderTOMLForScope(cfg, RenderScopeUser)
	if !strings.Contains(user, "[cli]") || !strings.Contains(user, "diff_fences = true") {
		t.Fatalf("user render missing diff_fences:\n%s", user)
	}
	project := RenderTOMLForScope(cfg, RenderScopeProject)
	if strings.Contains(project, "diff_fences") {
		t.Fatalf("project render contains user-global CLI config:\n%s", project)
	}
	// Off is the default, so an unset field renders no [cli] section at all.
	if off := RenderTOMLForScope(Default(), RenderScopeUser); strings.Contains(off, "diff_fences") {
		t.Fatalf("default render emitted diff_fences:\n%s", off)
	}
}
