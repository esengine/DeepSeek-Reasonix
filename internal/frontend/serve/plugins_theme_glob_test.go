package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestPluginThemeGlobInstallsBesideAuthorNotes(t *testing.T) {
	_, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"theme-glob-notes","version":"1.0.0","contributes":{"themes":["themes/*/theme.json"]}}`)
	writePluginFile(t, filepath.Join(source, "themes", "dawn", "theme.json"), `{"schemaVersion":1,"name":"Dawn","tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#111111"}}}`)
	const notes = "Author notes live beside the theme directories."
	writePluginFile(t, filepath.Join(source, "themes", "README.md"), notes)
	resp := postJSON(t, base+"/plugins/plan", map[string]any{"source": source})
	plan := decodeInstallSource(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan status=%d: %v", resp.StatusCode, plan)
	}
	resp = postJSON(t, base+"/plugins/install", map[string]any{"source": source, "planId": plan["planId"]})
	result := decodeInstallSource(t, resp)
	if resp.StatusCode != http.StatusOK || result["status"] != "done" {
		t.Fatalf("install status=%d: %v", resp.StatusCode, result)
	}
	plugins := getPlugins(t, base)
	if len(plugins) != 1 || len(plugins[0].Themes) != 1 || plugins[0].Error != "" || len(plugins[0].Warnings) != 0 {
		t.Fatalf("plugins=%+v", plugins)
	}
	find(t, listThemes(t, base), "plugin:theme-glob-notes:dawn")
	for _, root := range []string{source, plugins[0].Root} {
		raw, err := os.ReadFile(filepath.Join(root, "themes", "README.md"))
		if err != nil || string(raw) != notes {
			t.Fatalf("author notes at %s = %q, %v", root, raw, err)
		}
	}
}
