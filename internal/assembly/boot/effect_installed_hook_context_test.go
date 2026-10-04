package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectCopyInstalledHookContextLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, manifestPath, manifest, contextPath string
		startupContext                            bool
	}{
		{
			name: "native", manifestPath: pluginpkg.NativeManifest,
			manifest:    `{"apiVersion":"reasonix.io/plugin/v2","name":"context-kit","version":"1.0.0","contributes":{"hooks":{"SessionStart":[{"contextFile":"context/startup.md"}]}}}`,
			contextPath: filepath.Join("context", "startup.md"), startupContext: true,
		},
		{
			name: "codex", manifestPath: pluginpkg.CodexManifest,
			manifest:    `{"name":"context-kit","version":"1.0.0","skills":"./skills/"}`,
			contextPath: "CLAUDE.md", startupContext: true,
		},
		{
			name: "claude", manifestPath: pluginpkg.ClaudeManifest,
			manifest: `{"name":"context-kit","version":"1.0.0","skills":"./skills/"}`, contextPath: "CLAUDE.md",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			kind := "boot-installed-context-" + tc.name
			writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
			approveWorkspace(t, workspace)
			source := robustTempDir(t)
			contextBody := "COPY-INSTALLED " + tc.name + " STARTUP CONTEXT"
			writeFile(t, source, tc.manifestPath, tc.manifest)
			writeFile(t, source, tc.contextPath, contextBody)
			if tc.name != "native" {
				writeFile(t, source, filepath.Join("skills", "notes", "SKILL.md"), "---\nname: notes\ndescription: Write notes\n---\nWrite concise notes.")
			}
			installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
			runInstaller := func(request map[string]any) string {
				t.Helper()
				args, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				out, err := installer.Execute(t.Context(), args)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			request := map[string]any{"source": source, "kind": "plugin", "mode": "copy", "scope": "global"}
			for _, apply := range []bool{false, true} {
				request["apply"] = apply
				out := runInstaller(request)
				var result struct {
					OK      bool   `json:"ok"`
					Applied bool   `json:"applied"`
					PlanID  string `json:"planId"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply || result.PlanID == "" {
					t.Fatalf("install_source apply=%t: %s, err=%v", apply, out, err)
				}
				request["planId"] = result.PlanID
			}
			installed, found, err := pluginpkg.FindInstalled(reasonixHome, "context-kit")
			if err != nil || !found || !installed.Enabled {
				t.Fatalf("installed context kit = %+v, found=%t, err=%v", installed, found, err)
			}
			root := pluginpkg.ResolveRoot(reasonixHome, installed.Root)
			copied, err := os.ReadFile(filepath.Join(root, tc.contextPath))
			if err != nil || string(copied) != contextBody || root == source {
				t.Fatalf("copied context = %q, root=%q, err=%v", copied, root, err)
			}
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
			rec := &effectRecordingProvider{}
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			var prefix string
			runPhase := func(t *testing.T, enabled bool) {
				t.Helper()
				ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
				if err != nil {
					t.Fatal(err)
				}
				defer ctrl.Close()
				for turn, input := range []string{"first task", "next task"} {
					before := len(rec.requests())
					if err := ctrl.Run(t.Context(), input); err != nil {
						t.Fatal(err)
					}
					reqs := rec.requests()
					if len(reqs) <= before {
						t.Fatal("installed hook turn did not reach the provider")
					}
					last := reqs[len(reqs)-1]
					var user string
					for _, msg := range last.Messages {
						if msg.Role == provider.RoleUser {
							user = msg.Content
						}
					}
					wantContext := enabled && tc.startupContext && turn == 0
					if strings.Contains(user, contextBody) != wantContext || !strings.Contains(user, input) {
						t.Fatalf("latest provider user turn %d, enabled=%t:\n%s", turn, enabled, user)
					}
					if wantContext && (!strings.Contains(user, `<hook-context event="SessionStart">`) || strings.Count(user, contextBody) != 1) {
						t.Fatalf("installed context is not projected once as SessionStart context:\n%s", user)
					}
					current := systemMessage(last.Messages)
					if strings.Contains(current, contextBody) {
						t.Fatal("installed hook context leaked into the cached system prefix")
					}
					if prefix == "" {
						prefix = current
					} else if current != prefix {
						t.Fatal("hook package lifecycle changed the cached system prefix")
					}
				}
			}
			t.Run("installed", func(t *testing.T) { runPhase(t, true) })
			t.Run("restarted", func(t *testing.T) { runPhase(t, true) })
			if err := pluginpkg.SetEnabled(reasonixHome, "context-kit", false); err != nil {
				t.Fatal(err)
			}
			t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
			if err := pluginpkg.SetEnabled(reasonixHome, "context-kit", true); err != nil {
				t.Fatal(err)
			}
			t.Run("enabled_again", func(t *testing.T) { runPhase(t, true) })
			removed := runInstaller(map[string]any{"op": "uninstall", "kind": "plugin", "name": "context-kit", "scope": "global"})
			var result struct {
				OK      bool `json:"ok"`
				Applied bool `json:"applied"`
			}
			if err := json.Unmarshal([]byte(removed), &result); err != nil || !result.OK || !result.Applied {
				t.Fatalf("uninstall = %s, err=%v", removed, err)
			}
			if _, found, err := pluginpkg.FindInstalled(reasonixHome, "context-kit"); err != nil || found {
				t.Fatalf("removed context kit still registered, found=%t, err=%v", found, err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("context kit files remain after uninstall: %v", err)
			}
			t.Run("removed", func(t *testing.T) { runPhase(t, false) })
		})
	}
}
