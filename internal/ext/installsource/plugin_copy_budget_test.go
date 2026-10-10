package installsource

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginCopyUsesPackageBudget(t *testing.T) {
	root := testenv.TempDir(t)
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"presentations","skills":"./"}`)
	writeFile(t, filepath.Join(root, "presentations", "SKILL.md"), "---\nname: presentations\ndescription: Create presentations\n---\nCreate presentations.")
	asset := filepath.Join(root, "presentations", "assets.bin")
	writeFile(t, asset, "")
	if err := os.Truncate(asset, maxSkillCopyBytes+1); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testenv.TempDir(t), "presentations")
	if err := installCopiedPlugin(pkg, root, target, false, ""); err != nil {
		t.Fatalf("plugin above individual-skill budget failed: %v", err)
	}
	if info, err := os.Stat(filepath.Join(target, "presentations", "assets.bin")); err != nil || info.Size() != maxSkillCopyBytes+1 {
		t.Fatalf("copied asset = %v, error = %v", info, err)
	}
	if err := copyDir(root, testenv.TempDir(t), maxSkillCopyBytes, ""); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("individual skill over its budget returned %v", err)
	}
}

func TestPluginCopyOverPackageBudgetKeepsExistingInstall(t *testing.T) {
	root := testenv.TempDir(t)
	writeFile(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name":"presentations","skills":"./"}`)
	writeFile(t, filepath.Join(root, "presentations", "SKILL.md"), "---\nname: presentations\ndescription: Create presentations\n---\nCreate presentations.")
	asset := filepath.Join(root, "presentations", "assets.bin")
	writeFile(t, asset, "")
	if err := os.Truncate(asset, tarballTotalLimit+1); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := pluginpkg.ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	parent := testenv.TempDir(t)
	target := filepath.Join(parent, "presentations")
	writeFile(t, filepath.Join(target, "previous.txt"), "existing install")
	if err := installCopiedPlugin(pkg, root, target, true, ""); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("over-budget replacement returned %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(target, "previous.txt")); err != nil || string(body) != "existing install" {
		t.Fatalf("existing install = %q, error = %v", body, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "presentations" {
		t.Fatalf("staging cleanup left %v, error = %v", entries, err)
	}
}
