package serve

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledPluginReportsHookDeclarationWarnings(t *testing.T) {
	_, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	const manifest = `{"apiVersion":"reasonix.io/plugin/v2","name":"hook-warning-kit","contributes":{"hooks":{
"SesionStart":[{"contextFile":"note.md","match":"["}],
"PreToolUse":[{"contextFile":"note.md","match":"["}],
"SessionStart":[{"contextFile":"note.md","match":"["}]
}}}`
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), manifest)
	writePluginFile(t, filepath.Join(source, "note.md"), "Local author note")
	install := func(replace bool) {
		t.Helper()
		args := map[string]any{"source": source, "replace": replace}
		plan := postJSON(t, base+"/plugins/plan", args)
		body := decodeInstallSource(t, plan)
		if plan.StatusCode != http.StatusOK || body["planId"] == "" {
			t.Fatalf("plan status=%d: %v", plan.StatusCode, body)
		}
		args["planId"] = body["planId"]
		resp := postJSON(t, base+"/plugins/install", args)
		if body := decodeInstallSource(t, resp); resp.StatusCode != http.StatusOK || body["status"] != "done" {
			t.Fatalf("install status=%d: %v", resp.StatusCode, body)
		}
	}
	read := func() []string {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || plugins[0].Name != "hook-warning-kit" || len(plugins[0].Hooks) != 3 || plugins[0].Error != "" {
			t.Fatalf("plugins=%+v", plugins)
		}
		return plugins[0].Warnings
	}
	install(false)
	warnings := read()
	if len(warnings) != 2 || !strings.Contains(warnings[0], "hooks.PreToolUse[0]: invalid matcher regex") || !strings.Contains(warnings[1], `hooks.SesionStart: unknown event`) {
		t.Fatalf("hook warnings=%v", warnings)
	}
	for _, enabled := range []bool{false, true} {
		resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "hook-warning-kit", "enabled": enabled})
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("enabled status=%d", resp.StatusCode)
		}
		if got := read(); !slices.Equal(got, warnings) {
			t.Fatalf("enabled=%v warnings=%v", enabled, got)
		}
	}
	corrected := strings.Replace(manifest, `"SesionStart"`, `"Stop"`, 1)
	corrected = strings.ReplaceAll(corrected, `"match":"["`, `"match":"read_file"`)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), corrected)
	if got := read(); !slices.Equal(got, warnings) {
		t.Fatal("source edits changed the installed copy's warnings")
	}
	install(true)
	if got := read(); len(got) != 0 {
		t.Fatalf("corrected replacement warnings=%v", got)
	}
}
