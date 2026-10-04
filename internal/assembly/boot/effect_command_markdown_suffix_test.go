package boot

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/runtime/agent/testutil"
)

func TestEffectInstalledMixedCaseMarkdownCommandsReachProvider(t *testing.T) {
	registerBootTokenProfileTestProvider()
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace, source := robustTempDir(t), robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, ".reasonix/commands/review.md", "PROJECT REVIEW: $ARGUMENTS")
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "MARKDOWN CASE BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, workspace)
	writeFile(t, source, pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"case-kit","contributes":{"commands":["commands"],"prompts":["prompts"]}}`)
	writeFile(t, source, "commands/review.MD", "CASE REVIEW: $ARGUMENTS")
	writeFile(t, source, "prompts/checks/brief.Md", "CASE BRIEF: $ARGUMENTS")
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	for _, apply := range []bool{false, true} {
		args["apply"] = apply
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			OK      bool   `json:"ok"`
			Applied bool   `json:"applied"`
			Status  string `json:"status"`
			PlanID  string `json:"planId"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply {
			t.Fatalf("install_source=%s err=%v", out, err)
		}
		if !apply {
			if result.Status != "planned" || result.PlanID == "" {
				t.Fatalf("preview=%+v", result)
			}
			args["planId"] = result.PlanID
		} else if result.Status != "done" {
			t.Fatalf("apply=%+v", result)
		}
	}
	rec := testutil.NewMock("markdown-case", testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"})
	setBootTokenProfileTestProvider(t, rec)
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, SessionDir: filepath.Join(robustTempDir(t), "sessions")})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	for _, input := range []string{"/case-kit:review issue", "/case-kit:checks:brief ticket", "/review local"} {
		ctrl.Submit(input)
		deadline := time.Now().Add(30 * time.Second)
		for ctrl.Running() {
			if time.Now().After(deadline) {
				t.Fatal("command turn did not finish")
			}
			time.Sleep(time.Millisecond)
		}
	}
	requests := agentRequests(rec.Requests())
	if len(requests) != 3 {
		t.Fatalf("provider requests=%d, want three", len(requests))
	}
	for i, want := range []string{"CASE REVIEW: issue", "CASE BRIEF: ticket", "PROJECT REVIEW: local"} {
		var user string
		for _, message := range requests[i].Messages {
			if message.Role == provider.RoleUser {
				user = message.Content
			}
		}
		if !strings.Contains(user, want) {
			t.Errorf("turn %d missing rendered command %q: %s", i, want, user)
		}
		prefix := systemMessage(requests[i].Messages)
		if strings.Contains(prefix, "CASE REVIEW:") || strings.Contains(prefix, "CASE BRIEF:") || strings.Contains(prefix, "PROJECT REVIEW:") {
			t.Error("command body entered the stable prefix")
		}
	}
}
