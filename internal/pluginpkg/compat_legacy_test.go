package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegacyNativeManifestRejected pins the v2 one-shot switch: native
// manifests without apiVersion are refused at ParseDir. Migration still
// accepts them via ParseNativeForMigrate.
func TestLegacyNativeManifestRejected(t *testing.T) {
	root := t.TempDir()
	manifest := `{
  "name": "legacy-demo",
  "version": "0.3.1",
  "description": "Legacy manifest without apiVersion",
  "skills": ["skills"]
}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ParseDir(root)
	if err == nil || !strings.Contains(err.Error(), "missing apiVersion") {
		t.Fatalf("ParseDir legacy = %v, want missing apiVersion", err)
	}
	pkg, _, err := ParseNativeForMigrate(root)
	if err != nil {
		t.Fatalf("ParseNativeForMigrate: %v", err)
	}
	if pkg.Manifest.Name != "legacy-demo" {
		t.Fatalf("migrate parse name = %q", pkg.Manifest.Name)
	}
}

// TestV1NativeManifestRejected ensures every native parser refuses v1.
func TestV1NativeManifestRejected(t *testing.T) {
	root := t.TempDir()
	manifest := `{
  "apiVersion": "reasonix.io/plugin/v1",
  "name": "old",
  "version": "1.0.0"
}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ParseDir(root)
	if err == nil || !strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("ParseDir v1 = %v, want unsupported apiVersion", err)
	}
	if _, _, err := ParseNativeForMigrate(root); err == nil || !strings.Contains(err.Error(), "unsupported apiVersion") {
		t.Fatalf("ParseNativeForMigrate v1 = %v, want unsupported apiVersion", err)
	}
}

func TestLoadInstalledAutoMigratesManagedLegacyManifest(t *testing.T) {
	home := t.TempDir()
	root := InstallRoot(home, "legacy-demo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"name":"legacy-demo","version":"1.0.0"}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, InstalledPlugin{Name: "legacy-demo", Root: RelativeRoot(home, root), Enabled: true}); err != nil {
		t.Fatal(err)
	}

	installed, warnings := LoadInstalled(home)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "automatically migrated") {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(installed) != 1 || installed[0].Package.Manifest.APIVersion != ManifestAPIVersionV2 {
		t.Fatalf("installed = %#v", installed)
	}
	backup, err := os.ReadFile(filepath.Join(root, NativeManifest+".bak"))
	if err != nil || string(backup) != legacy {
		t.Fatalf("backup = %q, err=%v", backup, err)
	}
}

func TestLoadInstalledQuarantinesExternalLegacyManifestWithoutModifyingIt(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	legacy := `{"name":"dev-demo","version":"1.0.0"}`
	manifestPath := filepath.Join(root, NativeManifest)
	if err := os.WriteFile(manifestPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, InstalledPlugin{Name: "dev-demo", Root: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	installed, warnings := LoadInstalled(home)
	if len(installed) != 0 {
		t.Fatalf("external incompatible plugin loaded: %#v", installed)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], PluginStatusDisabledIncompatible) || !strings.Contains(warnings[0], "plugin migrate dev-demo --to-v2") {
		t.Fatalf("warnings = %v", warnings)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil || string(after) != legacy {
		t.Fatalf("external manifest changed: %q, err=%v", after, err)
	}
	state, err := LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Plugins) != 1 || state.Plugins[0].Status != PluginStatusDisabledIncompatible {
		t.Fatalf("plugin state = %#v", state.Plugins)
	}
}
