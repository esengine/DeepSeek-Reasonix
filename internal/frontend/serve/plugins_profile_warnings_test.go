package serve

import (
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func TestInstalledPluginReportsRejectedProfileDeclarations(t *testing.T) {
	home, _, base := pluginHome(t)
	source := testenv.TempDir(t)
	writePluginFile(t, filepath.Join(source, pluginpkg.NativeManifest), `{
  "apiVersion":"reasonix.io/plugin/v2","name":"profile-warning-kit",
  "contributes":{"skills":["skills"],"agents":["agents"]}
}`)
	const valid = "---\ndescription: Review selected files\ndelivery:\n  review-report: review\n---\nReview the files."
	writePluginFile(t, filepath.Join(source, "skills", "valid", "SKILL.md"), valid)
	writePluginFile(t, filepath.Join(source, "skills", "broken", "SKILL.md"), strings.Replace(valid, "review-report: review", "unknown: review", 1))
	writePluginFile(t, filepath.Join(source, "agents", "reviewer.md"), strings.Replace(valid, "review-report: review", "review-report: unsupported", 1))
	const authority = "---\ndescription: Review files\nauthority:\n  baseline: approved\n---\nReview."
	for _, path := range []string{"agents/directory/SKILL.md", "agents/group/nested/SKILL.md", "agents/group/flat.md"} {
		writePluginFile(t, filepath.Join(source, filepath.FromSlash(path)), authority)
	}
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
		if len(plugins) != 1 || len(plugins[0].Skills) != 2 || len(plugins[0].Agents) != 1 || plugins[0].Error != "" {
			t.Fatalf("plugins = %+v", plugins)
		}
		return plugins[0].Warnings
	}
	warnings := read()
	for _, want := range []string{"skills/broken/SKILL.md", `unknown delivery field "unknown"`, "agents/reviewer.md", `unsupported`, "accepted: review, security", "agents/directory/SKILL.md", "agents/group/nested/SKILL.md", "agents/group/flat.md", "`authority:` is host-owned"} {
		if !strings.Contains(strings.Join(warnings, "\n"), want) {
			t.Errorf("warnings=%v, missing %q", warnings, want)
		}
	}
	if len(warnings) != 5 {
		t.Errorf("warnings=%v, want five rejected profiles", warnings)
	}
	checkRuntime := func(corrected bool) {
		t.Helper()
		root := pluginpkg.InstallRoot(home, "profile-warning-kit")
		skRoot, agentRoot := filepath.Join(root, "skills"), filepath.Join(root, "agents")
		skKey, agentKey := config.CanonicalSkillPath(skRoot), config.CanonicalSkillPath(agentRoot)
		st := skill.New(skill.Options{
			HomeDir: testenv.TempDir(t), CustomPaths: []string{skRoot, agentRoot},
			PluginPaths:      map[string][]string{skKey: {"profile-warning-kit"}, agentKey: {"profile-warning-kit"}},
			PluginAgentPaths: map[string][]string{agentKey: {"profile-warning-kit"}},
			DisableBuiltins:  true, Stderr: io.Discard,
		})
		if _, ok := st.ReadSlash("profile-warning-kit:valid"); !ok {
			t.Fatal("valid profile was not loaded")
		}
		for _, name := range []string{"profile-warning-kit:broken", "profile-warning-kit:agent:reviewer", "profile-warning-kit:agent:directory", "profile-warning-kit:agent:nested", "profile-warning-kit:agent:flat"} {
			if _, ok := st.ReadSlash(name); ok != corrected {
				t.Errorf("%s loaded=%v, want %v", name, ok, corrected)
			}
		}
	}
	checkRuntime(false)
	for _, enabled := range []bool{false, true} {
		resp := postJSON(t, base+"/plugins/enabled", map[string]any{"name": "profile-warning-kit", "enabled": enabled})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("enabled=%v: status=%d", enabled, resp.StatusCode)
		}
		if got := read(); !slices.Equal(got, warnings) {
			t.Errorf("enabled=%v warnings changed: %v", enabled, got)
		}
	}
	writePluginFile(t, filepath.Join(source, "skills", "broken", "SKILL.md"), valid)
	writePluginFile(t, filepath.Join(source, "agents", "reviewer.md"), valid)
	for _, path := range []string{"agents/directory/SKILL.md", "agents/group/nested/SKILL.md", "agents/group/flat.md"} {
		writePluginFile(t, filepath.Join(source, filepath.FromSlash(path)), valid)
	}
	if got := read(); !slices.Equal(got, warnings) {
		t.Error("source edits changed the installed copy's warnings")
	}
	checkRuntime(false)
	install(true)
	if got := read(); len(got) != 0 {
		t.Fatalf("corrected replacement warnings = %v", got)
	}
	checkRuntime(true)
}
