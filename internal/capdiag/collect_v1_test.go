package capdiag_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/capdiag"
	"reasonix/internal/pluginpkg"
)

// TestPluginPackageV2FieldsAreReported pins the Manifest v2 additions to the
// plugins report: prompts/themes counts and the runtime flag, in both the
// JSON payload and the text renderer.
func TestPluginPackageV2FieldsAreReported(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", reasonixHome)

	pluginRoot := filepath.Join(reasonixHome, "plugins", "demo")
	write(t, filepath.Join(pluginRoot, pluginpkg.NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "demo",
  "contributes": {
    "prompts": ["prompts"],
    "themes": ["themes/*.reasonix-theme"]
  },
  "runtime": {"command": "${REASONIX_PLUGIN_ROOT}/bin/demo", "intercepts": ["input.receive"]}
}`)
	write(t, filepath.Join(pluginRoot, "prompts", "plan.md"), "---\ndescription: plan\n---\nPlan $ARGUMENTS\n")
	write(t, filepath.Join(pluginRoot, "themes", "neon.reasonix-theme"), "theme bytes")
	write(t, filepath.Join(pluginRoot, "bin", "demo"), "#!/bin/sh\n")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name: "demo", Root: "plugins/demo", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: reasonixHome,
	})
	if len(r.Plugins.Packages) != 1 {
		t.Fatalf("plugin packages = %+v, want demo", r.Plugins.Packages)
	}
	pkg := r.Plugins.Packages[0]
	if pkg.Prompts != 1 || pkg.Themes != 1 || !pkg.Runtime {
		t.Fatalf("package v1 fields = prompts:%d themes:%d runtime:%v, want 1/1/true", pkg.Prompts, pkg.Themes, pkg.Runtime)
	}
	if r.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want the unchanged schema v1", r.SchemaVersion)
	}
	text := capdiag.RenderText(r)
	for _, want := range []string{"prompts=1", "themes=1", "FULL TRUST"} {
		if !strings.Contains(text, want) {
			t.Fatalf("text report missing %q:\n%s", want, text)
		}
	}

	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"prompts":1`, `"themes":1`, `"runtime":true`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("package JSON missing %s: %s", key, raw)
		}
	}
}

// TestPluginPackageLegacyOmitsV1Fields pins the omitempty contract: legacy
// packages produce no new JSON keys, so schema v1 consumers see no change.
func TestPluginPackageLegacyOmitsV1Fields(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", reasonixHome)

	pluginRoot := filepath.Join(reasonixHome, "plugins", "legacy")
	write(t, filepath.Join(pluginRoot, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"legacy","skills":["skills"]}`)
	write(t, filepath.Join(pluginRoot, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nS\n")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name: "legacy", Root: "plugins/legacy", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: reasonixHome,
	})
	if len(r.Plugins.Packages) != 1 {
		t.Fatalf("plugin packages = %+v, want legacy", r.Plugins.Packages)
	}
	raw, err := json.Marshal(r.Plugins.Packages[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"prompts"`, `"themes"`, `"runtime"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("legacy package JSON should omit %s: %s", key, raw)
		}
	}
}
