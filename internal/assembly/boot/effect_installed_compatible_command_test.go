package boot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestEffectInstalledCompatibleCommandLifecycle(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			source := robustTempDir(t)
			if err := os.CopyFS(source, os.DirFS(filepath.Join("..", "..", "..", "examples", "command-notes-kit"))); err != nil {
				t.Fatal(err)
			}
			if kind == "codex" {
				if err := os.Rename(filepath.Join(source, ".claude-plugin"), filepath.Join(source, ".codex-plugin")); err != nil {
					t.Fatal(err)
				}
			}
			wantFile, err := os.ReadFile(filepath.Join(source, "commands", "note.md"))
			if err != nil {
				t.Fatal(err)
			}
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			var rec *scriptedCallProvider
			providerKind := "boot-compatible-command-example-" + kind
			provider.Register(providerKind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = %q
model = "x"
`, providerKind))
			writeFile(t, workspace, ".reasonix/commands/note.md", "---\ndescription: Project note\n---\nPROJECT NOTE: $ARGUMENTS")
			approveWorkspace(t, workspace)

			const commandName = "command-notes-kit:note"
			const input = "/" + commandName + " ISSUE-7 verified change"
			fragments := []string{
				"# Note from supplied facts",
				"Treat this request as the input: ISSUE-7 verified change",
				"The first argument is the issue identifier: ISSUE-7",
				"The second argument begins the supplied facts: verified",
				"A literal dollar sign in this template is written as $.",
			}
			var prefix, toolSchema string
			check := func(stage string, present bool) {
				t.Helper()
				t.Run(stage, func(t *testing.T) {
					rec = &scriptedCallProvider{calls: []scriptedCall{
						{"slash_command", `{"command":"list"}`},
						{"slash_command", `{"command":"command-notes-kit:note","arguments":"ISSUE-7 verified change"}`},
						{"slash_command", `{"command":"note","arguments":"PROJECT-CHECK"}`},
					}}
					ctrl, err := Build(t.Context(), Options{Sink: event.Discard})
					if err != nil {
						t.Fatal(err)
					}
					defer ctrl.Close()
					ctrl.EnsureSessionPath()
					found := false
					for _, cmd := range ctrl.Commands() {
						if cmd.Name != commandName {
							continue
						}
						found = true
						if cmd.Hidden || cmd.Plugin != "command-notes-kit" || cmd.ArgHint != "<issue> <verified facts>" || cmd.Description != "Draft a short note from supplied, verified facts." {
							t.Fatalf("installed command identity = %+v", cmd)
						}
					}
					if found != present {
						t.Fatalf("installed command present = %t, want %t", found, present)
					}
					menu := ctrl.CompletionData("en").Names
					listed := false
					projectListed := false
					for _, item := range menu {
						listed = listed || item.Label == "/"+commandName
						projectListed = projectListed || item.Label == "/note"
					}
					if listed != present || !projectListed {
						t.Fatalf("completion: package=%t project=%t, want package=%t", listed, projectListed, present)
					}
					rendered, ok := ctrl.CustomCommand(input)
					if ok != present {
						t.Fatalf("custom command found = %t, want %t", ok, present)
					}
					if present {
						for _, fragment := range fragments {
							if !strings.Contains(rendered, fragment) {
								t.Fatalf("custom command lost %q:\n%s", fragment, rendered)
							}
						}
					}
					if project, ok := ctrl.CustomCommand("/note PROJECT-CHECK"); !ok || project != "PROJECT NOTE: PROJECT-CHECK" {
						t.Fatalf("project command = %q, found=%t", project, ok)
					}
					if err := ctrl.Run(t.Context(), "inspect the installed command"); err != nil {
						t.Fatal(err)
					}
					if list := rec.resultOf(0); strings.Contains(list, "/"+commandName) != present || !strings.Contains(list, "/note") {
						t.Fatalf("model command list = %q, want package=%t", list, present)
					}
					if list := rec.resultOf(0); present && !strings.Contains(list, "<issue> <verified facts>") {
						t.Fatalf("model command list lost the argument hint: %q", list)
					}
					modelResult := rec.resultOf(1)
					if present {
						for _, fragment := range fragments {
							if !strings.Contains(modelResult, fragment) {
								t.Fatalf("model command lost %q:\n%s", fragment, modelResult)
							}
						}
					} else if strings.Contains(modelResult, fragments[0]) {
						t.Fatalf("absent command body reached the model:\n%s", modelResult)
					}
					if project := rec.resultOf(2); !strings.Contains(project, "PROJECT NOTE: PROJECT-CHECK") {
						t.Fatalf("model project command = %q", project)
					}
					rec.mu.Lock()
					beforeSubmit := len(rec.reqs)
					rec.mu.Unlock()
					typed := "/note PROJECT-TYPED"
					if present {
						typed = input
					}
					ctrl.Submit(typed)
					deadline := time.Now().Add(30 * time.Second)
					for ctrl.Running() {
						if time.Now().After(deadline) {
							t.Fatal("typed command did not finish")
						}
						time.Sleep(time.Millisecond)
					}
					rec.mu.Lock()
					requests := append([]provider.Request(nil), rec.reqs...)
					rec.mu.Unlock()
					if len(requests) <= beforeSubmit {
						t.Fatal("typed command did not reach the provider")
					}
					var user string
					for _, message := range requests[beforeSubmit].Messages {
						if message.Role == provider.RoleUser {
							user = message.Content
						}
					}
					if present {
						for _, fragment := range fragments {
							if !strings.Contains(user, fragment) {
								t.Fatalf("typed command lost %q at the provider:\n%s", fragment, user)
							}
						}
						if strings.Contains(user, "$ARGUMENTS") || strings.Contains(user, "$1") || strings.Contains(user, "$2") {
							t.Fatalf("typed command contains unsubstituted arguments:\n%s", user)
						}
					} else if !strings.Contains(user, "PROJECT NOTE: PROJECT-TYPED") {
						t.Fatalf("typed project command did not reach the provider:\n%s", user)
					}
					for _, req := range requests {
						system := systemMessage(req.Messages)
						if strings.Contains(system, fragments[0]) || strings.Contains(system, commandName) {
							t.Fatal("command content entered the cache-stable system prefix")
						}
						schema, err := json.Marshal(req.Tools)
						if err != nil {
							t.Fatal(err)
						}
						if prefix == "" {
							prefix, toolSchema = system, string(schema)
						} else if system != prefix || string(schema) != toolSchema {
							t.Fatal("command lifecycle changed the system prefix or provider tool schema")
						}
					}
				})
			}
			check("before-install", false)
			installer := installsource.NewTool(installsource.Options{ProjectRoot: workspace, HomeDir: home, RequireApprovedPlan: true})
			request := map[string]any{"source": source, "kind": "plugin", "mode": "copy"}
			type result struct {
				OK      bool   `json:"ok"`
				Status  string `json:"status"`
				Applied bool   `json:"applied"`
				PlanID  string `json:"planId"`
				Actions []struct {
					Action       string `json:"action"`
					ManifestKind string `json:"manifestKind"`
					CommandCount int    `json:"commandCount"`
				} `json:"actions"`
			}
			run := func(install bool) result {
				t.Helper()
				args, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				out, err := installer.Execute(t.Context(), args)
				if err != nil {
					t.Fatal(err)
				}
				var response result
				if err := json.Unmarshal([]byte(out), &response); err != nil || !response.OK {
					t.Fatalf("install = %s, err=%v", out, err)
				}
				if len(response.Actions) != 1 {
					t.Fatalf("action count = %+v", response.Actions)
				}
				if install && (response.Actions[0].ManifestKind != kind || response.Actions[0].CommandCount != 1) {
					t.Fatalf("command install action = %+v", response.Actions)
				}
				if !install && response.Actions[0].Action != "remove_plugin_package" {
					t.Fatalf("command removal action = %+v", response.Actions)
				}
				return response
			}
			preview := run(true)
			if preview.Applied || preview.Status != "planned" || preview.PlanID == "" {
				t.Fatalf("preview = %+v", preview)
			}
			if installed, warnings := pluginpkg.LoadInstalled(reasonixHome); len(installed) != 0 || len(warnings) != 0 {
				t.Fatalf("preview changed installed state: %+v, warnings=%v", installed, warnings)
			}
			request["apply"], request["planId"] = true, preview.PlanID
			if applied := run(true); !applied.Applied || applied.Status != "done" {
				t.Fatalf("apply = %+v", applied)
			}
			installed, warnings := pluginpkg.LoadInstalled(reasonixHome)
			if len(installed) != 1 || len(warnings) != 0 {
				t.Fatalf("installed packages = %+v, warnings=%v", installed, warnings)
			}
			root := installed[0].Package.Root
			if got, err := os.ReadFile(filepath.Join(root, "commands", "note.md")); err != nil || string(got) != string(wantFile) {
				t.Fatalf("copied command differs from the example: err=%v", err)
			}
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
			check("copied-source-removed", true)
			if err := pluginpkg.SetEnabled(reasonixHome, "command-notes-kit", false); err != nil {
				t.Fatal(err)
			}
			check("disabled", false)
			if _, err := os.Stat(filepath.Join(root, "commands", "note.md")); err != nil {
				t.Fatalf("disable removed the copied command: %v", err)
			}
			if err := pluginpkg.SetEnabled(reasonixHome, "command-notes-kit", true); err != nil {
				t.Fatal(err)
			}
			check("re-enabled", true)
			request = map[string]any{"op": "uninstall", "kind": "plugin", "name": "command-notes-kit"}
			if removed := run(false); !removed.Applied || removed.Status != "done" {
				t.Fatalf("removal = %+v", removed)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("removed copy still exists: %v", err)
			}
			check("removed", false)
		})
	}
}
