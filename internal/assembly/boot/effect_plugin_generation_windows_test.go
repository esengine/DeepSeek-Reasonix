package boot

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectWindowsPluginGenerationReachesProvider(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, reasonixHome, "config.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-plugin-generation"
model = "x"
`)
	approveWorkspace(t, workspace)
	source := robustTempDir(t)
	writeFile(t, source, pluginpkg.ClaudeManifest, `{"name":"generation-probe"}`)
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	for _, body := range []string{"OLD GENERATION BODY", "NEW GENERATION BODY"} {
		writeFile(t, source, "skills/probe/SKILL.md", "---\nname: probe\ndescription: Neutral probe\n---\n"+body)
		request := map[string]any{"source": source, "kind": "plugin", "replace": body == "NEW GENERATION BODY"}
		for _, apply := range []bool{false, true} {
			request["apply"] = apply
			args, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := installer.Execute(t.Context(), args)
			var result struct {
				OK      bool   `json:"ok"`
				Applied bool   `json:"applied"`
				PlanID  string `json:"planId"`
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply {
				t.Fatalf("install apply=%t: %s, error=%v", apply, out, err)
			}
			request["planId"] = result.PlanID
		}
	}
	approved := map[string]any{"source": source, "kind": "plugin", "replace": true}
	previewArgs, _ := json.Marshal(approved)
	preview, err := installer.Execute(t.Context(), previewArgs)
	if err != nil {
		t.Fatal(err)
	}
	var ticket struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal([]byte(preview), &ticket); err != nil {
		t.Fatal(err)
	}
	writeFile(t, source, "skills/probe/SKILL.md", "---\nname: probe\ndescription: Neutral probe\n---\nUNAPPROVED BODY")
	approved["apply"], approved["planId"] = true, ticket.PlanID
	changedArgs, _ := json.Marshal(approved)
	if _, err := installer.Execute(t.Context(), changedArgs); !errors.Is(err, installsource.ErrApprovalDenied) {
		t.Fatalf("changed source passed approval: %v", err)
	}
	rec := &effectRecordingProvider{}
	provider.Register("boot-plugin-generation", func(provider.Config) (provider.Provider, error) { return rec, nil })
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.Submit("/generation-probe:probe neutral task")
	deadline := time.Now().Add(30 * time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("updated plugin turn did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	requests := rec.requests()
	if len(requests) == 0 {
		t.Fatal("updated plugin did not reach provider")
	}
	last := requests[len(requests)-1]
	if strings.Contains(systemMessage(last.Messages), "GENERATION BODY") {
		t.Fatal("plugin body entered the stable prefix")
	}
	var user string
	for _, message := range last.Messages {
		if message.Role == provider.RoleUser {
			user = message.Content
		}
	}
	if !strings.Contains(user, "NEW GENERATION BODY") || strings.Contains(user, "OLD GENERATION BODY") {
		t.Fatalf("provider received the wrong generation: %s", user)
	}
}
