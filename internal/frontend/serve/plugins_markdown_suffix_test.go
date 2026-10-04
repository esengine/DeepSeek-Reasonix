package serve

import (
	"net/http"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledCommandAndPromptNamesDropMixedCaseMarkdownSuffix(t *testing.T) {
	_, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"case-kit","contributes":{"commands":["commands"],"prompts":["prompts"]}}`)
	writePluginFile(t, filepath.Join(source, "commands", "review.MD"), "REVIEW $ARGUMENTS")
	writePluginFile(t, filepath.Join(source, "prompts", "checks", "brief.Md"), "BRIEF $ARGUMENTS")
	args := map[string]any{"source": source}
	resp := postJSON(t, base+"/plugins/plan", args)
	plan := decodeInstallSource(t, resp)
	if resp.StatusCode != http.StatusOK || plan["planId"] == "" {
		t.Fatalf("plan status=%d: %v", resp.StatusCode, plan)
	}
	if got := getPlugins(t, base); len(got) != 0 {
		t.Fatalf("preview installed %+v", got)
	}
	args["planId"] = plan["planId"]
	resp = postJSON(t, base+"/plugins/install", args)
	if result := decodeInstallSource(t, resp); resp.StatusCode != http.StatusOK || result["status"] != "done" {
		t.Fatalf("install status=%d: %v", resp.StatusCode, result)
	}
	check := func() {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || len(plugins[0].Commands) != 1 || len(plugins[0].Prompts) != 1 {
			t.Fatalf("inventory=%+v", plugins)
		}
		p := plugins[0]
		if c := p.Commands[0]; c.Name != "review" || c.Invocation != "/case-kit:review" {
			t.Errorf("command=%+v", c)
		}
		if c := p.Prompts[0]; c.Name != "checks:brief" || c.Invocation != "/case-kit:checks:brief" {
			t.Errorf("prompt=%+v", c)
		}
	}
	check()
	resp = postJSON(t, base+"/plugins/enabled", map[string]any{"name": "case-kit", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d", resp.StatusCode)
	}
	check()
}
