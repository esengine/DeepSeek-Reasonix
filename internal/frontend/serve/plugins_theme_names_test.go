package serve

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledPluginListsThemeNamesMatchingPackAddresses(t *testing.T) {
	_, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"palette-duo","version":"1.0.0","contributes":{"themes":["themes/*/theme.json"]}}`)
	for _, name := range []string{"dawn", "midnight"} {
		writePluginFile(t, filepath.Join(source, "themes", name, "theme.json"), `{"schemaVersion":1,"id":"not-the-address","name":"Readable palette","tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#111111"}}}`)
	}
	install := func(replace bool) {
		t.Helper()
		args := map[string]any{"source": source, "replace": replace}
		resp := postJSON(t, base+"/plugins/plan", args)
		plan := decodeInstallSource(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("plan status=%d: %v", resp.StatusCode, plan)
		}
		args["planId"] = plan["planId"]
		resp = postJSON(t, base+"/plugins/install", args)
		if body := decodeInstallSource(t, resp); resp.StatusCode != http.StatusOK || body["status"] != "done" {
			t.Fatalf("install status=%d: %v", resp.StatusCode, body)
		}
	}
	names := func() []string {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || len(plugins[0].Themes) != 2 || plugins[0].Error != "" || len(plugins[0].Warnings) != 0 {
			t.Fatalf("plugins=%+v", plugins)
		}
		var out []string
		for _, item := range plugins[0].Themes {
			out = append(out, item.Name)
		}
		return out
	}
	install(false)
	want := []string{"dawn", "midnight"}
	installedNames := names()
	if got := installedNames; !reflect.DeepEqual(got, want) {
		t.Errorf("installed names=%v, want directory addresses %v", got, want)
	}
	for _, name := range want {
		find(t, listThemes(t, base), "plugin:palette-duo:"+name)
	}
	resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "palette-duo", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d", resp.StatusCode)
	}
	if got := names(); !reflect.DeepEqual(got, installedNames) {
		t.Errorf("disabled inventory names=%v, want %v", got, installedNames)
	}
	if err := os.Rename(filepath.Join(source, "themes", "dawn"), filepath.Join(source, "themes", "noon")); err != nil {
		t.Fatal(err)
	}
	if got := names(); !reflect.DeepEqual(got, installedNames) {
		t.Errorf("source edit changed installed-copy names=%v", got)
	}
	install(true)
	if got, want := names(), []string{"midnight", "noon"}; !reflect.DeepEqual(got, want) {
		t.Errorf("replacement names=%v, want %v", got, want)
	}
	resp = postJSON(t, base+"/plugins/enabled", map[string]any{"name": "palette-duo", "enabled": true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status=%d", resp.StatusCode)
	}
	listed := listThemes(t, base)
	for _, pack := range listed {
		if pack.ID == "plugin:palette-duo:dawn" {
			t.Fatal("the replaced theme directory's address remained listed")
		}
	}
	for _, name := range []string{"midnight", "noon"} {
		find(t, listed, "plugin:palette-duo:"+name)
	}
}
