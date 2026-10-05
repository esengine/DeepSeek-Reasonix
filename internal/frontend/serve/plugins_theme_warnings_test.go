package serve

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/theme"
)

func TestInstalledPluginReportsThemeValidationWarnings(t *testing.T) {
	_, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{
  "apiVersion":"reasonix.io/plugin/v2","name":"theme-warning-kit","version":"1.0.0",
  "contributes":{"themes":["themes/good/theme.json","themes/broken/theme.json","themes/partial/theme.json","themes/ignored.txt"]}
}`)
	const valid = `{"schemaVersion":1,"name":"Readable","tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#111111"}}}`
	writePluginFile(t, filepath.Join(source, "themes", "good", "theme.json"), valid)
	writePluginFile(t, filepath.Join(source, "themes", "broken", "theme.json"), strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":99`, 1))
	writePluginFile(t, filepath.Join(source, "themes", "partial", "theme.json"), `{"schemaVersion":1,"tokens":{"light":{"bg":"#FFFFFF","fg":"not-a-colour","sidebar":"#111111"},"dark":{"bg":"#111111"}}}`)
	writePluginFile(t, filepath.Join(source, "themes", "ignored.txt"), "Not a theme manifest.")
	install := func(replace bool) {
		t.Helper()
		args := map[string]any{"source": source, "replace": replace}
		plan := postJSON(t, base+"/plugins/plan", args)
		if plan.StatusCode != http.StatusOK {
			t.Fatalf("plan status=%d: %v", plan.StatusCode, decodeInstallSource(t, plan))
		}
		args["planId"] = decodeInstallSource(t, plan)["planId"]
		resp := postJSON(t, base+"/plugins/install", args)
		if body := decodeInstallSource(t, resp); resp.StatusCode != http.StatusOK || body["status"] != "done" {
			t.Fatalf("install status=%d: %v", resp.StatusCode, body)
		}
	}
	install(false)
	read := func() []string {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || plugins[0].Name != "theme-warning-kit" || len(plugins[0].Themes) != 4 || plugins[0].Error != "" {
			t.Fatalf("plugins = %+v", plugins)
		}
		return plugins[0].Warnings
	}
	warnings := read()
	if len(warnings) != 3 {
		t.Errorf("warnings=%v, want one unreadable theme and two dropped tokens", warnings)
	}
	for _, want := range []string{"themes/broken/theme.json", "unsupported schemaVersion 99", "themes/partial/theme.json", `"fg"`, `"sidebar"`} {
		if !strings.Contains(strings.Join(warnings, "\n"), want) {
			t.Errorf("warnings=%v, missing %q", warnings, want)
		}
	}
	if _, err := theme.Load("plugin:theme-warning-kit:broken"); err == nil {
		t.Fatal("the unsupported theme loaded")
	}
	partial, err := theme.Load("plugin:theme-warning-kit:partial")
	if err != nil || len(partial.Warnings) != 2 || partial.Tokens["light"]["fg"] != "" {
		t.Fatalf("partial theme = %+v, %v", partial, err)
	}
	find(t, listThemes(t, base), "plugin:theme-warning-kit:good")
	for _, enabled := range []bool{false, true} {
		resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "theme-warning-kit", "enabled": enabled})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("enabled=%v: status=%d", enabled, resp.StatusCode)
		}
		if got := read(); !slices.Equal(got, warnings) {
			t.Errorf("enabled=%v warnings changed: %v", enabled, got)
		}
	}
	for _, name := range []string{"broken", "partial"} {
		writePluginFile(t, filepath.Join(source, "themes", name, "theme.json"), valid)
	}
	if got := read(); !slices.Equal(got, warnings) {
		t.Error("source edits changed the installed copy's warnings")
	}
	install(true)
	if got := read(); len(got) != 0 {
		t.Fatalf("corrected replacement warnings = %v", got)
	}
	find(t, listThemes(t, base), "plugin:theme-warning-kit:broken")
}
