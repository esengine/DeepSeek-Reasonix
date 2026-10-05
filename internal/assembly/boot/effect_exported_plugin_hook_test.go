package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectExportedPluginHookReachesProvider(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not store POSIX execute bits")
	}
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "EXPORT HOOK BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-exported-hook"
model = "x"
`)
	approveWorkspace(t, workspace)
	source := robustTempDir(t)
	writeFile(t, source, pluginpkg.NativeManifest, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "exported-hook",
  "version": "1.0.0",
  "contributes": {
    "hooks": {"SessionStart": [{"command": "hooks/start", "args": []}]}
  }
}`)
	const hookContext = "Exported hook context."
	writeFile(t, source, "hooks/start", "#!/bin/sh\nprintf '%s\\n' '"+hookContext+"'\n")
	if err := os.Chmod(filepath.Join(source, "hooks", "start"), 0o755); err != nil {
		t.Fatal(err)
	}
	installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
	install := func(args map[string]any) string {
		t.Helper()
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
		if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK {
			t.Fatalf("install_source = %s, err=%v", out, err)
		}
		if args["apply"] == true || args["op"] == "uninstall" {
			if !result.Applied || result.Status != "done" {
				t.Fatalf("install_source did not apply: %s", out)
			}
		} else if result.Applied || result.Status != "planned" || result.PlanID == "" {
			t.Fatalf("install_source did not preview: %s", out)
		}
		return result.PlanID
	}
	var recorder *effectRecordingProvider
	provider.Register("boot-exported-hook", func(provider.Config) (provider.Provider, error) { return recorder, nil })
	var prefix, schema string
	runPhase := func(t *testing.T, enabled bool) {
		t.Helper()
		recorder = &effectRecordingProvider{}
		ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		ctrl.EnsureSessionPath()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for _, prompt := range []string{"first turn", "follow-up turn"} {
			if err := ctrl.Run(ctx, prompt); err != nil {
				t.Fatal(err)
			}
		}
		reqs := agentRequests(recorder.requests())
		if len(reqs) != 2 {
			t.Fatalf("provider requests = %d, want 2", len(reqs))
		}
		for i, req := range reqs {
			latest := ""
			for _, msg := range slices.Backward(req.Messages) {
				if msg.Role == provider.RoleUser {
					latest = msg.Content
					break
				}
			}
			wantCount := 0
			if enabled && i == 0 {
				wantCount = 1
			}
			block := "<hook-context event=\"SessionStart\">\n" + hookContext + "\n</hook-context>"
			if got := strings.Count(latest, block); got != wantCount {
				t.Fatalf("turn %d context count = %d, want %d; latest user message = %q", i, got, wantCount, latest)
			}
			tools, err := json.Marshal(req.Tools)
			if err != nil {
				t.Fatal(err)
			}
			system := systemMessage(req.Messages)
			if prefix == "" {
				prefix, schema = system, string(tools)
			}
			if system != prefix || string(tools) != schema {
				t.Fatal("hook lifecycle changed the provider prefix or tool schema")
			}
		}
		if got := recorder.rawUserInputs(); !slices.Equal(got, []string{"first turn", "follow-up turn"}) {
			t.Fatalf("raw user inputs = %q", got)
		}
	}
	t.Run("absent", func(t *testing.T) { runPhase(t, false) })
	args := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
	args["planId"] = install(args)
	args["apply"] = true
	install(args)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	t.Run("original-install", func(t *testing.T) { runPhase(t, true) })
	root := pluginpkg.InstallRoot(reasonixHome, "exported-hook")
	archive, required, err := pluginpkg.Export("exported-hook", root)
	if err != nil || len(required) != 0 {
		t.Fatalf("export: required=%v, err=%v", required, err)
	}
	zipPath := filepath.Join(robustTempDir(t), "exported-hook.zip")
	if err := os.WriteFile(zipPath, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	install(map[string]any{"op": "uninstall", "name": "exported-hook", "scope": "global"})
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original installation still exists: %v", err)
	}
	args = map[string]any{"source": zipPath, "kind": "plugin", "scope": "global"}
	args["planId"] = install(args)
	t.Run("zip-preview", func(t *testing.T) { runPhase(t, false) })
	args["apply"] = true
	install(args)
	if err := os.Remove(zipPath); err != nil {
		t.Fatal(err)
	}
	t.Run("reinstalled", func(t *testing.T) { runPhase(t, true) })
	if err := pluginpkg.SetEnabled(reasonixHome, "exported-hook", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "hooks", "start")); err != nil {
		t.Fatalf("disable removed installed hook: %v", err)
	}
	t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
	if err := pluginpkg.SetEnabled(reasonixHome, "exported-hook", true); err != nil {
		t.Fatal(err)
	}
	t.Run("reenabled", func(t *testing.T) { runPhase(t, true) })
	install(map[string]any{"op": "uninstall", "name": "exported-hook", "scope": "global"})
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed installation still exists: %v", err)
	}
	t.Run("removed", func(t *testing.T) { runPhase(t, false) })
}
