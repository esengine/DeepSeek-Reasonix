package boot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/hook"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

func TestEffectIsolatedWindowsHookCannotMutateSource(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	marker := filepath.Join(root, "hook-escape.txt")
	command := "Set-Content -LiteralPath '" + strings.ReplaceAll(marker, "'", "''") + "' -Value escaped"
	settings, err := json.Marshal(hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{
		hook.PostToolUse: {{Match: "write_file", Command: command}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(config.RootsForHome(home).Home(), hook.SettingsFilename)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, settings, 0600); err != nil {
		t.Fatal(err)
	}
	if loaded := hook.Load(hook.LoadOptions{ReasonixHomeDir: config.RootsForHome(home).Home(), ProjectRoot: root}); len(loaded) != 1 {
		t.Fatalf("fixture did not load the real user hook: %+v", loaded)
	}
	managed := filepath.Join(config.RootsForHome(home).DeliveryWorktreeDir(), "isolated")
	p := &environmentScriptProvider{isolationScriptProvider: isolationScriptProvider{dir: root, managed: managed}}
	provider.Register("boot-isolated-hook-integrity", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-isolated-hook-integrity"
model = "x"
supported_efforts = ["high"]
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "source")
	var mu sync.Mutex
	var failedHook bool
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeHookFailed && strings.Contains(e.Detail, sandbox.ErrIncompleteIntegrity.Error()) {
			mu.Lock()
			failedHook = true
			mu.Unlock()
		}
	})
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctrl.Close)
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := ctrl.Run(t.Context(), "delegate an isolated writer with a program hook"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	observed := failedHook
	mu.Unlock()
	if !observed {
		t.Fatal("real child hook did not report the missing integrity boundary")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hook mutated source: %v", err)
	}
	catalog, err := ctrl.IsolationResults(t.Context())
	if err != nil || len(catalog.Results) != 1 {
		t.Fatalf("file-tool result was not retained: %+v %v", catalog, err)
	}
}

func TestEffectIsolatedWindowsShellCannotMutateSource(t *testing.T) {
	for _, operation := range []string{"create", "delete", "rename"} {
		{
			name := operation
			t.Run(name, func(t *testing.T) {
				home, root := statedBootHome(t), testenv.TempDir(t)
				victim := filepath.Join(root, "victim.txt")
				quote := func(path string) string { return "'" + strings.ReplaceAll(path, "'", "''") + "'" }
				commands := map[string]string{
					"create": "Set-Content -LiteralPath " + quote(filepath.Join(root, "unexpected.txt")) + " -Value escaped",
					"delete": "Remove-Item -LiteralPath " + quote(victim),
					"rename": "Rename-Item -LiteralPath " + quote(victim) + " -NewName renamed.txt",
				}
				p := &isolatedIntegrityProvider{command: commands[operation]}
				kind := "isolated-integrity-" + operation
				provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
				writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[sandbox]
windows_integrity = true
[tools.shell]
prefer = "powershell"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
				writeFile(t, root, "victim.txt", "source survives\n")
				mirrorToUserConfigAt(t, home, root)
				gitInDir(t, root, "init", "-q")
				gitInDir(t, root, "add", "-A")
				gitInDir(t, root, "commit", "-qm", "source")
				var code string
				sink := event.FuncSink(func(e event.Event) {
					if e.Kind == event.ToolResult && e.Tool.Name == "bash" {
						code = e.Tool.RefusalCode
					}
				})
				ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(ctrl.Close)
				ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
				if err := ctrl.Run(t.Context(), "delegate an isolated integrity fixture"); err != nil {
					t.Fatal(err)
				}
				p.mu.Lock()
				result := p.result
				p.mu.Unlock()
				if code != "sandbox.incomplete_integrity" {
					t.Fatalf("isolated shell was not refused before launch: code=%q result=%s", code, result)
				}
				if body, err := os.ReadFile(victim); err != nil || string(body) != "source survives\n" {
					t.Fatalf("source changed: %q %v", body, err)
				}
				for _, file := range []string{"unexpected.txt", "renamed.txt"} {
					if _, err := os.Lstat(filepath.Join(root, file)); !os.IsNotExist(err) {
						t.Fatalf("escaped mutation %s: %v", file, err)
					}
				}
				catalog, err := ctrl.IsolationResults(t.Context())
				if err != nil || catalog.RecoveryRequired {
					t.Fatalf("refusal left an invalid isolated environment: %+v %v", catalog, err)
				}
			})
		}
	}
}
