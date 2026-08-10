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
