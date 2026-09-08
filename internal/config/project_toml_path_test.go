package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectTOMLPathPrefersRootThenNested(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, ".reasonix", "reasonix.toml")
	primary := filepath.Join(root, "reasonix.toml")

	if got := projectTOMLPath(root); got != primary {
		t.Fatalf("missing both: projectTOMLPath = %q, want conventional %q", got, primary)
	}

	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("default_model = \"nested/model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := projectTOMLPath(root); got != nested {
		t.Fatalf("nested only: projectTOMLPath = %q, want %q", got, nested)
	}

	if err := os.WriteFile(primary, []byte("default_model = \"root/model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := projectTOMLPath(root); got != primary {
		t.Fatalf("both present: projectTOMLPath = %q, want root %q", got, primary)
	}
}

func TestLoadForRootReadsNestedProjectTOML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "rx-home"))
	t.Setenv("REASONIX_STATE_HOME", "")

	root := t.TempDir()
	nested := filepath.Join(root, ".reasonix", "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("default_model = \"nested/flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	if got := cfg.DefaultModel; got != "nested/flash" {
		t.Fatalf("DefaultModel = %q, want nested/flash from .reasonix/reasonix.toml", got)
	}
	if got := SourcePathForRoot(root); got != nested {
		t.Fatalf("SourcePathForRoot = %q, want %q", got, nested)
	}
}

func TestLoadForRootRootTOMLWinsOverNested(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "rx-home"))
	t.Setenv("REASONIX_STATE_HOME", "")

	root := t.TempDir()
	nested := filepath.Join(root, ".reasonix", "reasonix.toml")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("default_model = \"nested/flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte("default_model = \"root/flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	if got := cfg.DefaultModel; got != "root/flash" {
		t.Fatalf("DefaultModel = %q, want root/flash", got)
	}
}
