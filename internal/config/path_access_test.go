package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSaveToPreservesMultiLevelSymlinkChain(t *testing.T) {
	home := t.TempDir()
	targetDir := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	target := filepath.Join(targetDir, "target.toml")
	first := filepath.Join(targetDir, "first.toml")
	second := UserConfigPath()
	if err := os.WriteFile(target, []byte("default_model = \"old\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, first); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := os.Symlink(first, second); err != nil {
		t.Skipf("symlink chains are unavailable: %v", err)
	}

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveConfigAccessPath(second, true)
	if err != nil {
		t.Fatalf("resolveConfigAccessPath(second): %v", err)
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(resolvedTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("resolveConfigAccessPath(second) = %q, want same file as %q", got, resolvedTarget)
	}

	cfg := Default()
	cfg.DefaultModel = "deepseek-pro"
	if err := cfg.SaveTo(second); err != nil {
		t.Fatalf("SaveTo through symlink chain: %v", err)
	}
	for name, path := range map[string]string{"first": first, "second": second} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("Lstat(%s): %v", name, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("SaveTo replaced the %s symlink", name)
		}
	}
	var persisted Config
	if _, err := toml.DecodeFile(target, &persisted); err != nil {
		t.Fatalf("decode target: %v", err)
	}
	if persisted.DefaultModel != "deepseek-pro" {
		t.Fatalf("target default_model = %q, want deepseek-pro", persisted.DefaultModel)
	}
}
