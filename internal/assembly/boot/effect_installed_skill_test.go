package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
)

func TestEffectInstalledSkillReachesProvider(t *testing.T) {
	home := isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-installed-skill"
model = "x"
`)
	approveWorkspace(t, workspace)
	source := filepath.Join(robustTempDir(t), "SKILL.md")
	if err := os.WriteFile(source, []byte("---\nname: ecosystem-probe\ndescription: Ecosystem probe\n---\nECOSYSTEM SKILL BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home})
	type installResult struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Applied bool   `json:"applied"`
		Actions []struct {
			Action string `json:"action"`
			Status string `json:"status"`
			Name   string `json:"name"`
		} `json:"actions"`
	}
	runInstall := func(apply bool) installResult {
		t.Helper()
		args, _ := json.Marshal(map[string]any{"source": source, "kind": "skill", "scope": "global", "apply": apply})
		out, err := installer.Execute(t.Context(), args)
		if err != nil {
			t.Fatalf("install_source apply=%t: %v", apply, err)
		}
		var result installResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("install_source response %q: %v", out, err)
		}
		if !result.OK {
			t.Fatalf("install_source apply=%t: %s", apply, out)
		}
		return result
	}
	preview := runInstall(false)
	if preview.Status != "planned" || preview.Applied || len(preview.Actions) != 1 || preview.Actions[0].Action != "copy_skill" {
		t.Fatalf("preview = %+v", preview)
	}
	installed := runInstall(true)
	if installed.Status != "done" || !installed.Applied || len(installed.Actions) != 1 || installed.Actions[0].Status != "done" {
		t.Fatalf("installation = %+v", installed)
	}

	var rec *effectRecordingProvider
	provider.Register("boot-effect-installed-skill", func(provider.Config) (provider.Provider, error) { return rec, nil })
	rec = &effectRecordingProvider{}
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build after installation: %v", err)
	}
	defer ctrl.Close()
	if sent, ok := ctrl.RunSkill("/ecosystem-probe task"); !ok || !strings.Contains(sent, "ECOSYSTEM SKILL BODY") {
		t.Fatalf("installed RunSkill = %q, %t", sent, ok)
	}
	ctrl.Submit("/ecosystem-probe task")
	deadline := time.Now().Add(30 * time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("installed skill turn did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	requests := rec.requests()
	if len(requests) == 0 {
		t.Fatal("installed skill did not reach provider")
	}
	request := requests[len(requests)-1]
	if strings.Contains(systemMessage(request.Messages), "ECOSYSTEM SKILL BODY") {
		t.Fatal("installed skill body leaked into cache-stable prefix")
	}
	var user string
	for _, message := range request.Messages {
		if message.Role == provider.RoleUser {
			user = message.Content
		}
	}
	if !strings.Contains(user, "<skill-pin name=\"ecosystem-probe\">") || !strings.Contains(user, "ECOSYSTEM SKILL BODY") {
		t.Fatalf("installed skill is absent from provider user message:\n%s", user)
	}
}
