package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

// installV2RuntimePlugin writes a Manifest v2 package with a runtime into
// the test Reasonix home and registers it in plugin state.
func installV2RuntimePlugin(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, "plugins", "example")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "example",
  "version": "1.0.0",
  "contributes": {
    "prompts": ["prompts"],
    "themes": ["themes/*.reasonix-theme"]
  },
  "runtime": {
    "command": "${REASONIX_PLUGIN_ROOT}/bin/example",
    "args": ["--serve"],
    "required": true,
    "intercepts": ["input.receive", "tool.before"],
    "replaces": ["system_prompt"],
    "capabilities": ["interceptors", "ui"]
  }
}`)
	writePluginTestFile(t, filepath.Join(root, "prompts", "plan.md"), "---\ndescription: plan\n---\nPlan $ARGUMENTS")
	writePluginTestFile(t, filepath.Join(root, "themes", "neon.reasonix-theme"), "theme bytes")
	writePluginTestFile(t, filepath.Join(root, "bin", "example"), "#!/bin/sh\n")
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name: "example", Root: "plugins/example", Version: "1.0.0", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPluginShowRendersRuntimeFullTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	installV2RuntimePlugin(t, home)

	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"show", "example"}); rc != 0 {
			t.Fatalf("plugin show rc = %d, want 0", rc)
		}
	})
	for _, want := range []string{
		"prompts: 1",
		"themes: 1",
		"runtime: FULL TRUST",
		"command: ${REASONIX_PLUGIN_ROOT}/bin/example --serve",
		"intercepts: input.receive, tool.before",
		"replaces: system_prompt",
		"capabilities: interceptors, ui",
		"bypass permissions",
		"prompts:\n  plan",
		"themes:\n  neon",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plugin show output missing %q:\n%s", want, out)
		}
	}
}

func TestPluginDoctorValidatesV2Runtime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	installV2RuntimePlugin(t, home)

	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"doctor", "example"}); rc != 0 {
			t.Fatalf("plugin doctor rc = %d, want 0", rc)
		}
	})
	if !strings.Contains(out, "runtime: FULL TRUST") || !strings.Contains(out, "ok: example") {
		t.Fatalf("doctor output missing runtime block or ok line:\n%s", out)
	}
}

func TestPluginDoctorFailsForMissingRuntimeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := installV2RuntimePlugin(t, home)
	// Remove the runtime binary so the command no longer resolves.
	if err := os.Remove(filepath.Join(root, "bin", "example")); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		if rc := pluginCommand([]string{"doctor", "example"}); rc != 1 {
			t.Fatalf("plugin doctor rc = %d, want 1 for a missing runtime command", rc)
		}
	})
	if !strings.Contains(stderr, "runtime command not found:") {
		t.Fatalf("doctor stderr missing the runtime command diagnostic:\n%s", stderr)
	}
}

func TestPluginDoctorReportsMissingV2PathsAsWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "gap")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "gap",
  "contributes": {
    "themes": ["themes/*.reasonix-theme"]
  }
}`)
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name: "gap", Root: "plugins/gap", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"doctor", "gap"}); rc != 0 {
			t.Fatalf("plugin doctor rc = %d, want 0 (missing themes are warnings, not failures)", rc)
		}
	})
	if !strings.Contains(out, `theme glob "themes/*.reasonix-theme" matched no files`) {
		t.Fatalf("doctor output missing the glob warning:\n%s", out)
	}
}
