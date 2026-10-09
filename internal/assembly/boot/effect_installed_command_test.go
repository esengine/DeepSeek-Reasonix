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
	"reasonix/internal/session/control"
)

func TestEffectCopyInstalledCommandLifecycle(t *testing.T) {
	const qualified = "command-kit:prepare"
	const template = "---\ndescription: Prepare copied notes\nargument-hint: <first> [rest]\n---\nINSTALLED COMMAND BODY: $ARGUMENTS\nfirst=$1; second=$2; literal=$$"
	const rendered = "INSTALLED COMMAND BODY: alpha beta\nfirst=alpha; second=beta; literal=$"
	for _, tc := range []struct{ name, manifestPath, manifest string }{
		{"native", pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"command-kit","version":"1.0.0","contributes":{"commands":["commands"]}}`},
		{"codex", pluginpkg.CodexManifest, `{"name":"command-kit","version":"1.0.0","commands":"./commands/"}`},
		{"claude", pluginpkg.ClaudeManifest, `{"name":"command-kit","version":"1.0.0","commands":"./commands/"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			kind := "boot-installed-command-" + tc.name
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
			writeFile(t, source, tc.manifestPath, tc.manifest)
			commandPath := filepath.Join("commands", "prepare.md")
			writeFile(t, source, commandPath, template)
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
					Actions []struct {
						CommandCount int `json:"commandCount"`
					} `json:"actions"`
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil || !result.OK || result.Applied != apply || result.PlanID == "" || len(result.Actions) != 1 || result.Actions[0].CommandCount != 1 {
					t.Fatalf("install_source apply=%t: %s, err=%v", apply, out, err)
				}
				request["planId"] = result.PlanID
			}
			installed, found, err := pluginpkg.FindInstalled(reasonixHome, "command-kit")
			if err != nil || !found || !installed.Enabled {
				t.Fatalf("installed command kit = %+v, found=%t, err=%v", installed, found, err)
			}
			root := pluginpkg.ResolveRoot(reasonixHome, installed.Root)
			copied, err := os.ReadFile(filepath.Join(root, commandPath))
			if err != nil || string(copied) != template || root == source {
				t.Fatalf("copied command = %q, root=%q, err=%v", copied, root, err)
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
				visible := false
				for _, item := range ctrl.CompletionData("en").Names {
					if item.Label == "/prepare" {
						t.Fatal("hidden command compatibility alias appears in completion")
					}
					if item.Label == "/"+qualified {
						visible = true
						if item.Kind != "command" || item.Insert != "/"+qualified+" " {
							t.Fatalf("installed command completion = %+v", item)
						}
					}
				}
				if visible != enabled {
					t.Fatalf("command completion visible=%t, enabled=%t", visible, enabled)
				}
				for _, name := range []string{qualified, "prepare"} {
					input := "/" + name + " alpha beta"
					sent, found := ctrl.CustomCommand(input)
					if found != enabled || (enabled && sent != rendered) {
						t.Fatalf("CustomCommand %q = %q, found=%t, enabled=%t", input, sent, found, enabled)
					}
					before := len(rec.requests())
					ctrl.SubmitHTTPOptions(input, control.SubmitOptions{RefuseUnknownSlash: true})
					if !enabled {
						if ctrl.Running() || len(rec.requests()) != before {
							t.Fatal("disabled or removed command started a provider turn")
						}
						continue
					}
					waitForCond(t, "installed command provider request", 10*time.Second, func() bool { return len(rec.requests()) > before })
					waitForCond(t, "installed command completion", 10*time.Second, func() bool { return !ctrl.Running() })
					reqs := rec.requests()
					last := reqs[len(reqs)-1]
					var user string
					for _, msg := range last.Messages {
						if msg.Role == provider.RoleUser {
							user = msg.Content
						}
					}
					if !strings.Contains(user, rendered) || strings.Contains(user, "$ARGUMENTS") || strings.Contains(user, "$1") || strings.Contains(user, "$2") {
						t.Fatalf("installed command arguments did not reach the provider:\n%s", user)
					}
					current := systemMessage(last.Messages)
					if strings.Contains(current, "INSTALLED COMMAND BODY") {
						t.Fatal("installed command body leaked into the cached system prefix")
					}
					if prefix == "" {
						prefix = current
					} else if current != prefix {
						t.Fatal("installed command restart changed the cached system prefix")
					}
				}
			}
			t.Run("installed", func(t *testing.T) { runPhase(t, true) })
			t.Run("restarted", func(t *testing.T) { runPhase(t, true) })
			if err := pluginpkg.SetEnabled(reasonixHome, "command-kit", false); err != nil {
				t.Fatal(err)
			}
			t.Run("disabled", func(t *testing.T) { runPhase(t, false) })
			if err := pluginpkg.SetEnabled(reasonixHome, "command-kit", true); err != nil {
				t.Fatal(err)
			}
			t.Run("enabled_again", func(t *testing.T) { runPhase(t, true) })
			removed := runInstaller(map[string]any{"op": "uninstall", "kind": "plugin", "name": "command-kit", "scope": "global"})
			var result struct {
				OK      bool `json:"ok"`
				Applied bool `json:"applied"`
			}
			if err := json.Unmarshal([]byte(removed), &result); err != nil || !result.OK || !result.Applied {
				t.Fatalf("uninstall = %s, err=%v", removed, err)
			}
			if _, found, err := pluginpkg.FindInstalled(reasonixHome, "command-kit"); err != nil || found {
				t.Fatalf("removed command kit still registered, found=%t, err=%v", found, err)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("command kit files remain after uninstall: %v", err)
			}
			t.Run("removed", func(t *testing.T) { runPhase(t, false) })
		})
	}
}
