package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginDoctorReportsHookDeclarationWarningsWhenDisabled(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "hook-warning-kit")
	const manifest = `{"apiVersion":"reasonix.io/plugin/v2","name":"hook-warning-kit","contributes":{"hooks":{
"SesionStart":[{"contextFile":"note.md"}],
"PreToolUse":[{"contextFile":"note.md","match":"["},{"contextFile":"note.md","match":"*"}],
"PostToolUse":[{"contextFile":"note.md","match":"["}],
"PostToolUseFailure":[{"contextFile":"note.md","match":"["}],
"PermissionRequest":[{"contextFile":"note.md","match":"["}],
"SessionStart":[{"contextFile":"note.md","match":"["}]
}}}`
	path := filepath.Join(root, pluginpkg.NativeManifest)
	writePluginTestFile(t, path, manifest)
	writePluginTestFile(t, filepath.Join(root, "note.md"), "Local author note")
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "hook-warning-kit", Root: "plugins/hook-warning-kit", ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"doctor", "hook-warning-kit"}); rc != 0 {
				t.Fatalf("doctor rc=%d", rc)
			}
		})
		if strings.Count(out, "warning:") != 5 {
			t.Fatalf("enabled=%v doctor=%s", enabled, out)
		}
		for _, event := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionRequest"} {
			if !strings.Contains(out, "hooks."+event+"[0]: invalid matcher regex") {
				t.Errorf("enabled=%v missing %s warning: %s", enabled, event, out)
			}
		}
		if !strings.Contains(out, "hooks.SesionStart: unknown event") || strings.Contains(out, "hooks.SessionStart") {
			t.Fatalf("unknown or non-tool event warnings incorrect: %s", out)
		}
	}
	corrected := strings.Replace(manifest, `"SesionStart"`, `"Stop"`, 1)
	corrected = strings.ReplaceAll(corrected, `"match":"["`, `"match":"read_file"`)
	writePluginTestFile(t, path, corrected)
	out := captureStdout(t, func() {
		if rc := pluginCommand([]string{"doctor", "hook-warning-kit"}); rc != 0 {
			t.Fatalf("corrected doctor rc=%d", rc)
		}
	})
	if strings.Contains(out, "warning:") {
		t.Fatalf("corrected declarations still have warnings: %s", out)
	}
}
