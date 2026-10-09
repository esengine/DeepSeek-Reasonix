package serve

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func TestPluginInstalledAgentDirectoryMatchesRuntime(t *testing.T) {
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"directory-agents","contributes":{"agents":["agents"]}}`)
	writePluginFile(t, filepath.Join(source, "agents", "group", "review", "SKILL.md"), "---\nname: inspect\ndescription: Inspect selected files\nmodel: custom-model\ntools: [Read, Grep]\n---\nInspect the selected files.")
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
	store := func() *skill.Store {
		t.Helper()
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		return skill.New(skill.Options{
			HomeDir: home, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(),
			PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(),
			DisableBuiltins: true, Stderr: io.Discard,
		})
	}
	profile, ok := store().ReadSlash("directory-agents:agent:inspect")
	if !ok || profile.RunAs != skill.RunSubagent || profile.Invocation != "manual" {
		t.Fatalf("runtime profile=%+v, found=%t", profile, ok)
	}
	if profile.Path != filepath.Join(pluginpkg.InstallRoot(home, "directory-agents"), "agents", "group", "review", "SKILL.md") {
		t.Fatalf("runtime profile path=%q", profile.Path)
	}
	checkInventory := func(enabled bool) {
		t.Helper()
		plugins := getPlugins(t, base)
		if len(plugins) != 1 || plugins[0].Enabled != enabled || len(plugins[0].Agents) != 1 {
			t.Fatalf("plugins=%+v", plugins)
		}
		agent := plugins[0].Agents[0]
		if agent.Name != profile.Name || agent.Description != profile.Description || agent.Invocation != "/"+profile.SlashName() {
			t.Fatalf("inventory agent=%+v, runtime profile=%+v", agent, profile)
		}
	}
	checkInventory(true)
	resp = postJSON(t, base+"/plugins/enabled", map[string]any{"name": "directory-agents", "enabled": false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status=%d", resp.StatusCode)
	}
	checkInventory(false)
	if profile, ok := store().ReadSlash("directory-agents:agent:inspect"); ok {
		t.Fatalf("disabled profile remains available: %+v", profile)
	}
}

func TestPluginInstalledUnicodeDirectoryAgentMatchesRuntime(t *testing.T) {
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, "reasonix-plugin.json"), `{"apiVersion":"reasonix.io/plugin/v2","name":"unicode-directory","contributes":{"agents":["agents"]}}`)
	for _, name := range []string{"中文", "cafe\u0301"} {
		writePluginFile(t, filepath.Join(source, "agents", "group", name, "SKILL.md"), "---\ndescription: Directory fixture\n---\nReview.")
	}
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
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	store := skill.New(skill.Options{HomeDir: home, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(),
		PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(), DisableBuiltins: true, Stderr: io.Discard})
	profiles := map[string]skill.Skill{}
	for _, name := range []string{"中文", "café"} {
		profile, ok := store.ReadSlash("unicode-directory:agent:" + name)
		if !ok || profile.RunAs != skill.RunSubagent || profile.Invocation != "manual" {
			t.Fatalf("runtime profile=%+v, found=%t", profile, ok)
		}
		profiles[name] = profile
	}
	plugins := getPlugins(t, base)
	if len(plugins) != 1 || len(plugins[0].Agents) != 2 {
		t.Fatalf("plugins=%+v", plugins)
	}
	for _, agent := range plugins[0].Agents {
		profile, ok := profiles[agent.Name]
		if !ok || agent.Invocation != "/"+profile.SlashName() || agent.Description != profile.Description {
			t.Fatalf("inventory agent=%+v, runtime=%+v", agent, profile)
		}
		delete(profiles, agent.Name)
	}
	if len(profiles) != 0 {
		t.Fatalf("profiles omitted from inventory: %v", profiles)
	}
}
