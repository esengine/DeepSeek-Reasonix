package serve

import (
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/pluginpkg"
)

func TestInstalledPluginReportsFileContributionRootWarnings(t *testing.T) {
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"root-warning-kit","contributes":{"skills":["skills/sample.md"],"agents":["agents/sample.md"],"commands":["commands/sample.md"],"prompts":["prompts/sample.md"]}}`)
	for _, kind := range []string{"skills", "agents", "commands", "prompts"} {
		writePluginFile(t, filepath.Join(source, kind, "sample.md"), "---\nname: sample\ndescription: A sample\n---\nSample body")
	}
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
	read := func() []string {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || plugins[0].Error != "" || len(plugins[0].Commands) != 0 || len(plugins[0].Prompts) != 0 || len(plugins[0].Agents) != 0 {
			t.Fatalf("plugins=%+v", plugins)
		}
		return plugins[0].Warnings
	}
	warnings := read()
	if len(warnings) != 4 {
		t.Errorf("warnings=%v, want four unusable-root warnings", warnings)
	}
	for _, kind := range []string{"skills", "agents", "commands", "prompts"} {
		if !strings.Contains(strings.Join(warnings, "\n"), kind+" path \""+kind+"/sample.md\" is not a directory") {
			t.Errorf("warnings omit %s: %v", kind, warnings)
		}
	}
	root := pluginpkg.InstallRoot(home, "root-warning-kit")
	commands, err := command.LoadRoots(
		command.Root{Path: filepath.Join(root, "commands", "sample.md"), Plugin: "root-warning-kit"},
		command.Root{Path: filepath.Join(root, "prompts", "sample.md"), Plugin: "root-warning-kit"},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range commands {
		if cmd.Plugin == "root-warning-kit" {
			t.Fatalf("a file root unexpectedly became a runnable command: %+v", cmd)
		}
	}
	resp = postJSON(t, base+"/plugins/enabled", map[string]any{"name": "root-warning-kit", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d", resp.StatusCode)
	}
	if got := read(); !slices.Equal(got, warnings) {
		t.Errorf("disabled inventory warnings=%v, want %v", got, warnings)
	}
}
