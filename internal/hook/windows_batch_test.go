//go:build windows

package hook

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
	"reasonix/internal/sandbox"
)

func TestDefaultSpawnerRunsQuotedPluginBatchHook(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugin root")
	hooksDir := filepath.Join(pluginRoot, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(hooksDir, "run-hook.cmd")
	// Use raw %1 rather than %~1 so the test catches accidental argument
	// re-quoting; %~1 would hide added surrounding quotes.
	contents := "@echo off\r\nset /p hook_input=\r\necho %1:%hook_input%\r\n"
	if err := os.WriteFile(script, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name    string
		command string
		args    []string
		mode    ExecutionMode
	}{
		{name: "shell form", command: `"` + filepath.ToSlash(script) + `" session-start`},
		{name: "explicit default shell form", command: `"` + filepath.ToSlash(script) + `" session-start`, mode: ExecutionShell},
		{name: "argv form", command: filepath.ToSlash(script), args: []string{"session-start"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultSpawner(context.Background(), SpawnInput{
				Command: tt.command,
				Args:    tt.args,
				Mode:    tt.mode,
				Stdin:   `{"event":"SessionStart"}`,
				Timeout: realSpawnTimeout,
			})
			if result.ExitCode != 0 || result.SpawnErr != nil {
				t.Fatalf("batch hook failed: %+v", result)
			}
			if got, want := result.Stdout, `session-start:{"event":"SessionStart"}`; got != want {
				t.Fatalf("batch hook stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestSuperpowersV611SessionStartHookEndToEnd(t *testing.T) {
	home := t.TempDir()
	installSuperpowersV611HookFixture(t, home)
	workspace := filepath.Join(home, "workspace")
	hooks := Load(LoadOptions{HomeDir: home, ProjectRoot: workspace})
	if len(hooks) != 1 {
		t.Fatalf("hooks = %+v, want one superpowers hook", hooks)
	}

	report := Run(context.Background(), Payload{
		Event:     SessionStart,
		SessionID: "issue-6602",
		Cwd:       workspace,
	}, hooks, nil)
	if report.Blocked || len(report.Outcomes) != 1 {
		t.Fatalf("SessionStart report = %+v", report)
	}
	outcome := report.Outcomes[0]
	if outcome.Decision != DecisionPass || outcome.ExitCode != 0 || outcome.Stderr != "" || outcome.TimedOut {
		t.Fatalf("SessionStart outcome = %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Stdout, "session-start:") ||
		!strings.Contains(outcome.Stdout, `"hook_event_name":"SessionStart"`) ||
		!strings.Contains(outcome.Stdout, `"session_id":"issue-6602"`) {
		t.Fatalf("SessionStart stdout = %q, want batch argument plus Claude-compatible stdin", outcome.Stdout)
	}
}

func TestExtensionlessSessionStartHookUsesAutoResolvedBash(t *testing.T) {
	shell := sandbox.ResolveShell("auto", "", nil)
	if shell.Kind != sandbox.ShellBash || strings.TrimSpace(shell.Path) == "" {
		t.Skip("Git Bash is not available on this Windows test host")
	}

	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "superpowers")
	writeHookTestFile(t, filepath.Join(root, pluginpkg.CodexManifest), `{
  "name": "superpowers",
  "version": "6.1.1"
}`)
	writeHookTestFile(t, filepath.Join(root, "hooks", "session-start-codex"), `#!/usr/bin/env bash
input=$(cat)
printf 'session-start:%s' "$input"
`)
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "6.1.1",
		ManifestKind: "codex",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(home, "workspace")
	hooks := Load(LoadOptions{HomeDir: home, ProjectRoot: workspace})
	if len(hooks) != 1 {
		t.Fatalf("hooks = %+v, want one extensionless SessionStart hook", hooks)
	}
	if hooks[0].ExecutionMode != ExecutionShell || hooks[0].Shell != "bash" {
		t.Fatalf("hook execution contract = mode %q shell %q, want auto-resolved Bash", hooks[0].ExecutionMode, hooks[0].Shell)
	}

	report := Run(context.Background(), Payload{
		Event:     SessionStart,
		SessionID: "auto-bash",
		Cwd:       workspace,
	}, hooks, NewDefaultSpawner(RuntimeOptions{BashPath: shell.Path}))
	if report.Blocked || len(report.Outcomes) != 1 {
		t.Fatalf("SessionStart report = %+v", report)
	}
	outcome := report.Outcomes[0]
	if outcome.Decision != DecisionPass || outcome.ExitCode != 0 || outcome.Stderr != "" || outcome.TimedOut {
		t.Fatalf("SessionStart outcome = %+v", outcome)
	}
	if !strings.HasPrefix(outcome.Stdout, "session-start:") || !strings.Contains(outcome.Stdout, `"event":"SessionStart"`) {
		t.Fatalf("SessionStart stdout = %q, want native JSON payload", outcome.Stdout)
	}
}

func TestProjectAndGlobalExtensionlessHooksUseAutoResolvedBash(t *testing.T) {
	shell := sandbox.ResolveShell("auto", "", nil)
	if shell.Kind != sandbox.ShellBash || strings.TrimSpace(shell.Path) == "" {
		t.Skip("Git Bash is not available on this Windows test host")
	}

	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")
	projectScript := filepath.Join(workspace, "hooks", "project-start")
	globalScript := filepath.Join(workspace, "hooks", "global-start")
	for _, item := range []struct {
		path   string
		prefix string
	}{
		{path: projectScript, prefix: "project"},
		{path: globalScript, prefix: "global"},
	} {
		writeHookTestFile(t, item.path, "#!/usr/bin/env bash\ninput=$(cat)\nprintf '"+item.prefix+":%s' \"$input\"\n")
	}
	writeHookTestFile(t, filepath.Join(workspace, ".reasonix", "settings.json"), `{"hooks":{"SessionStart":[{"command":"hooks/project-start"}]}}`)
	reasonixHome := filepath.Join(home, ".reasonix")
	writeHookTestFile(t, filepath.Join(reasonixHome, "settings.json"), `{"hooks":{"SessionStart":[{"command":"hooks/global-start"}]}}`)

	hooks := Load(LoadOptions{HomeDir: home, ProjectRoot: workspace})
	if len(hooks) != 2 {
		t.Fatalf("hooks = %+v, want project and global hooks", hooks)
	}
	report := Run(context.Background(), Payload{
		Event:     SessionStart,
		SessionID: "settings-bash",
		Cwd:       workspace,
	}, hooks, NewDefaultSpawner(RuntimeOptions{BashPath: shell.Path}))
	if report.Blocked || len(report.Outcomes) != 2 {
		t.Fatalf("SessionStart report = %+v", report)
	}
	for i, wantPrefix := range []string{"project:", "global:"} {
		outcome := report.Outcomes[i]
		if outcome.Decision != DecisionPass || outcome.ExitCode != 0 || outcome.Stderr != "" || outcome.TimedOut {
			t.Fatalf("SessionStart outcome[%d] = %+v", i, outcome)
		}
		if !strings.HasPrefix(outcome.Stdout, wantPrefix) || !strings.Contains(outcome.Stdout, `"event":"SessionStart"`) {
			t.Fatalf("SessionStart stdout[%d] = %q, want %q plus native JSON payload", i, outcome.Stdout, wantPrefix)
		}
	}
}

func TestDefaultSpawnerRunsCompoundCmdShellHook(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugin root")
	if err := os.MkdirAll(pluginRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(pluginRoot, "compound-hook.cmd")
	if err := os.WriteFile(script, []byte("@echo off\r\necho script:%1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := DefaultSpawner(context.Background(), SpawnInput{
		Command: `"` + filepath.ToSlash(script) + `" "argument with spaces" && echo chained`,
		Mode:    ExecutionShell,
		Shell:   "cmd",
		Timeout: realSpawnTimeout,
	})
	if result.ExitCode != 0 || result.SpawnErr != nil {
		t.Fatalf("compound cmd hook failed: %+v", result)
	}
	got := strings.ReplaceAll(result.Stdout, "\r\n", "\n")
	// %1 preserves the quoting required to keep the spaced argument together.
	// A batch script that wants the dequoted value uses %~1 instead.
	if got != "script:\"argument with spaces\"\nchained" {
		t.Fatalf("compound cmd stdout = %q", got)
	}
}

func TestDefaultSpawnerRunsPowerShellHookWithNestedQuotes(t *testing.T) {
	result := DefaultSpawner(context.Background(), SpawnInput{
		Command: `$items = @("a b", "c'd", 'e"f', "中文", "🧪"); Write-Output ($items -join "|")`,
		Mode:    ExecutionShell,
		Shell:   "powershell",
		Timeout: realSpawnTimeout,
	})
	if result.ExitCode != 0 || result.SpawnErr != nil {
		t.Fatalf("PowerShell hook failed: %+v", result)
	}
	if got, want := result.Stdout, `a b|c'd|e"f|中文|🧪`; got != want {
		t.Fatalf("PowerShell stdout = %q, want %q", got, want)
	}
}

func TestDefaultSpawnerCmdShellExpandsEnvironmentAndPipes(t *testing.T) {
	result := DefaultSpawner(context.Background(), SpawnInput{
		Command: `echo %HOOK_CMD_VALUE% | findstr cmd-ok`,
		Mode:    ExecutionShell,
		Shell:   "cmd",
		Env:     map[string]string{"HOOK_CMD_VALUE": "cmd-ok"},
		Timeout: realSpawnTimeout,
	})
	if result.ExitCode != 0 || result.SpawnErr != nil || !strings.Contains(result.Stdout, "cmd-ok") {
		t.Fatalf("cmd shell hook failed: %+v", result)
	}
}
