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
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/runtime/agent/testutil"
)

func TestEffectWorkNotePromptExampleLifecycle(t *testing.T) {
	registerBootTokenProfileTestProvider()
	example, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "work-note-kit"))
	if err != nil {
		t.Fatal(err)
	}
	source := robustTempDir(t)
	if err := os.CopyFS(source, os.DirFS(example)); err != nil {
		t.Fatal(err)
	}
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, ".reasonix/commands/handoff.md", "PROJECT HANDOFF $ARGUMENTS")
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "PROMPT EXAMPLE BASE"
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
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	type result struct {
		OK      bool   `json:"ok"`
		Applied bool   `json:"applied"`
		Status  string `json:"status"`
		PlanID  string `json:"planId"`
	}
	install := func(args map[string]any) result {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		out, err := installer.Execute(t.Context(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var r result
		if err := json.Unmarshal([]byte(out), &r); err != nil || !r.OK {
			t.Fatalf("install_source=%s, err=%v", out, err)
		}
		return r
	}
	check := func(t *testing.T, present, updated bool) {
		t.Helper()
		rec := testutil.NewMock("work-note-example", testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"}, testutil.Turn{Text: "ok"})
		setBootTokenProfileTestProvider(t, rec)
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard, SessionDir: filepath.Join(robustTempDir(t), "sessions")})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		if got, found := ctrl.CustomCommand("/handoff local"); !found || got != "PROJECT HANDOFF local" {
			t.Fatalf("project short command=%q, %v", got, found)
		}
		for _, name := range []string{"handoff", "checks:summarize"} {
			if _, found := ctrl.CustomCommand("/work-note-kit:" + name); found != present {
				t.Fatalf("%s found=%v, want %v", name, found, present)
			}
		}
		if !present {
			if len(rec.Requests()) != 0 {
				t.Fatal("discovering an absent template sent a provider request")
			}
			return
		}
		ctrl.EnsureSessionPath()
		for _, input := range []string{
			"/work-note-kit:handoff lint-passed integration-not-run",
			"/work-note-kit:checks:summarize build unit-passed network-not-run",
			"/work-note-kit:handoff",
		} {
			ctrl.Submit(input)
			deadline := time.Now().Add(30 * time.Second)
			for ctrl.Running() {
				if time.Now().After(deadline) {
					t.Fatal("prompt example turn did not finish")
				}
				time.Sleep(time.Millisecond)
			}
		}
		requests := agentRequests(rec.Requests())
		if len(requests) != 3 {
			t.Fatalf("provider requests=%d, want three", len(requests))
		}
		for i, req := range requests {
			var user string
			for _, msg := range req.Messages {
				if msg.Role == provider.RoleUser {
					user = msg.Content
				}
			}
			if strings.Contains(systemMessage(req.Messages), "Facts supplied by the caller:") || strings.Contains(systemMessage(req.Messages), "A literal dollar sign is") {
				t.Fatal("template body entered the stable system prefix")
			}
			if i == 1 {
				for _, want := range []string{"Label: build", "Observations: build unit-passed network-not-run", "A literal dollar sign is $."} {
					if !strings.Contains(user, want) {
						t.Fatalf("check template missing %q: %s", want, user)
					}
				}
			} else {
				if !strings.Contains(user, "Draft a short work handoff") || !strings.Contains(user, "name the missing facts") {
					t.Fatalf("handoff instructions missing: %s", user)
				}
				if i == 0 && !strings.Contains(user, "Facts supplied by the caller:\nlint-passed integration-not-run") {
					t.Fatalf("handoff arguments missing: %s", user)
				}
				if strings.Contains(user, "SOURCE UPDATE MARKER") != updated {
					t.Fatalf("source update presence=%v, want %v", strings.Contains(user, "SOURCE UPDATE MARKER"), updated)
				}
			}
			if strings.Contains(user, "$ARGUMENTS") || strings.Contains(user, "Label: $1") {
				t.Fatal("unrendered template tokens reached the provider")
			}
		}
	}
	t.Run("absent", func(t *testing.T) { check(t, false, false) })
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	preview := install(args)
	if preview.Applied || preview.Status != "planned" || preview.PlanID == "" {
		t.Fatalf("preview=%+v", preview)
	}
	t.Run("preview", func(t *testing.T) { check(t, false, false) })
	args["apply"], args["planId"] = true, preview.PlanID
	if applied := install(args); !applied.Applied || applied.Status != "done" {
		t.Fatalf("applied=%+v", applied)
	}
	t.Run("copied", func(t *testing.T) { check(t, true, false) })
	path := filepath.Join(source, "prompts", "handoff.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(contents, []byte("\nSOURCE UPDATE MARKER\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Run("source-edit", func(t *testing.T) { check(t, true, false) })
	args["replace"], args["apply"] = true, false
	delete(args, "planId")
	preview = install(args)
	if preview.Applied || preview.Status != "planned" {
		t.Fatalf("replacement preview=%+v", preview)
	}
	t.Run("replacement-preview", func(t *testing.T) { check(t, true, false) })
	args["apply"], args["planId"] = true, preview.PlanID
	if applied := install(args); !applied.Applied || applied.Status != "done" {
		t.Fatalf("replacement=%+v", applied)
	}
	t.Run("replaced", func(t *testing.T) { check(t, true, true) })
	for _, enabled := range []bool{false, true} {
		if err := pluginpkg.SetEnabled(reasonixHome, "work-note-kit", enabled); err != nil {
			t.Fatal(err)
		}
		if enabled {
			t.Run("reenabled", func(t *testing.T) { check(t, true, true) })
		} else {
			t.Run("disabled", func(t *testing.T) { check(t, false, false) })
		}
	}
	if removed := install(map[string]any{"op": "uninstall", "name": "work-note-kit", "scope": "global"}); !removed.Applied || removed.Status != "done" {
		t.Fatalf("removed=%+v", removed)
	}
	if _, err := os.Stat(pluginpkg.InstallRoot(reasonixHome, "work-note-kit")); !os.IsNotExist(err) {
		t.Fatalf("managed copy survives removal: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removal deleted original source: %v", err)
	}
	t.Run("removed", func(t *testing.T) { check(t, false, false) })
}
