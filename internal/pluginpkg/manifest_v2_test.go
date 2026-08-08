package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestV2RequiresProvides(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "deps",
  "version": "2.0.0",
  "requires": [
    {
      "namespace": "reasonix",
      "kind": "provider",
      "id": "deepseek/v4",
      "versionRange": ">=1.0.0",
      "optional": false
    }
  ],
  "provides": [
    {
      "namespace": "plugin/deps",
      "kind": "provider",
      "id": "fake/x",
      "version": "1.0.0",
      "schemaHash": "sha256:abc"
    }
  ]
}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Manifest.Requires) != 1 || len(pkg.Manifest.Provides) != 1 {
		t.Fatalf("requires/provides = %d/%d", len(pkg.Manifest.Requires), len(pkg.Manifest.Provides))
	}
	if err := pkg.ProvidesCapabilities()[0].Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestManifestV2ProviderRequiresSchemaHash(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "bad",
  "provides": [
    {"namespace": "plugin/bad", "kind": "provider", "id": "x", "version": "1.0.0"}
  ]
}`)
	_, _, err := ParseDir(root)
	if err == nil || !strings.Contains(err.Error(), "schemaHash") {
		t.Fatalf("err = %v, want schemaHash", err)
	}
}

func TestMigrateLegacyManifestToV2(t *testing.T) {
	root := t.TempDir()
	legacy := `{
  "name": "oldplug",
  "version": "1.2.3",
  "skills": []
}`
	if err := os.WriteFile(filepath.Join(root, NativeManifest), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseNativeForMigrate(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MigrateManifestToV2(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMigratedManifestV2(root, data); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, NativeManifest+".bak")); err != nil {
		t.Fatal(err)
	}
	got, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("parse migrated: %v", err)
	}
	if got.Manifest.APIVersion != ManifestAPIVersionV2 || got.Manifest.Name != "oldplug" {
		t.Fatalf("migrated = %+v", got.Manifest)
	}
}

func TestMigrateProvidersRequiresExplicitProvides(t *testing.T) {
	pkg := Package{ManifestKind: "reasonix", Manifest: Manifest{
		Name: "p", Version: "1.0.0",
		Runtime: &RuntimeSpec{Command: "./x", Capabilities: []string{"providers"}},
	}}
	if _, err := MigrateManifestToV2(pkg); err == nil || !strings.Contains(err.Error(), "cannot infer schemaHash") {
		t.Fatalf("err = %v", err)
	}
}

func TestMigrateRejectsVersionedManifest(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "already-v2"
}`)
	if _, _, err := ParseNativeForMigrate(root); err == nil || !strings.Contains(err.Error(), "already uses reasonix.io/plugin/v2") {
		t.Fatalf("ParseNativeForMigrate v2 = %v, want already-v2 rejection", err)
	}

	pkg := Package{ManifestKind: "reasonix", Manifest: Manifest{
		APIVersion: ManifestAPIVersionV2,
		Name:       "already-v2",
	}}
	if _, err := MigrateManifestToV2(pkg); err == nil || !strings.Contains(err.Error(), "without apiVersion") {
		t.Fatalf("MigrateManifestToV2 v2 = %v, want versioned rejection", err)
	}
}
