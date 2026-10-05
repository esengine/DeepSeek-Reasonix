package serve

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledPluginsListDistinctQualifiedPromptInvocations(t *testing.T) {
	_, _, base := pluginHome(t)
	for _, name := range []string{"notes-kit", "release-kit"} {
		source := testenv.TempDir(t)
		writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), fmt.Sprintf(`{"apiVersion":"reasonix.io/plugin/v2","name":%q,"contributes":{"prompts":["prompts"]}}`, name))
		writePluginFile(t, filepath.Join(source, "prompts", "review", "brief.md"), "---\ndescription: Summarize a note\n---\nSummarize $ARGUMENTS")
		args := map[string]any{"source": source}
		resp := postJSON(t, base+"/plugins/plan", args)
		plan := decodeInstallSource(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("plan status=%d: %v", resp.StatusCode, plan)
		}
		args["planId"] = plan["planId"]
		resp = postJSON(t, base+"/plugins/install", args)
		if result := decodeInstallSource(t, resp); resp.StatusCode != http.StatusOK || result["status"] != "done" {
			t.Fatalf("install status=%d: %v", resp.StatusCode, result)
		}
	}
	check := func() {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 2 {
			t.Fatalf("plugins=%+v", plugins)
		}
		for _, p := range plugins {
			if len(p.Prompts) != 1 {
				t.Fatalf("prompts=%+v", p.Prompts)
			}
			item := p.Prompts[0]
			if item.Name != "review:brief" || item.Invocation != "/"+p.Name+":review:brief" || item.Description != "Summarize a note" {
				t.Errorf("%s prompt=%+v", p.Name, item)
			}
		}
	}
	check()
	resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "notes-kit", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d", resp.StatusCode)
	}
	check()
}
