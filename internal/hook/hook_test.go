package hook

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/pluginpkg"
)

func writeSettings(t *testing.T, dir, json string) {
	t.Helper()
	d := filepath.Join(dir, SettingsDirname)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, SettingsFilename), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHookTestFile(t *testing.T, path, body string) {
	t.Helper()
	writeHookTestBytes(t, path, []byte(body))
}

func writeHookTestBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextFileUsableRequiresReadableRegularFile(t *testing.T) {
	root := t.TempDir()
	if ContextFileUsable("") {
		t.Fatal("empty context path should be unusable")
	}
	if ContextFileUsable(root) {
		t.Fatal("context directory should be unusable")
	}
	path := filepath.Join(root, "CLAUDE.md")
	writeHookTestFile(t, path, "Use the bundled workflow.")
	if !ContextFileUsable(path) {
		t.Fatal("readable regular context file should be usable")
	}
}

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
}

// realSpawnTimeout bounds tests that assert a real child process completes.
// Generous on purpose: node cold starts and first-run scans of a freshly
// built test binary can stall for seconds on a loaded machine, and these
// tests assert behavior, not latency. Tests asserting the timeout path keep
// their own tight budgets — that direction cannot flake under load.
const realSpawnTimeout = 15 * time.Second

const sampleSettings = `{"hooks":{"PreToolUse":[{"match":"bash","command":"echo pre"}],"Stop":[{"command":"echo stop"}]}}`

func hookSettingsWithCommand(t *testing.T, event Event, command string) string {
	t.Helper()
	body, err := json.Marshal(Settings{Hooks: map[Event][]HookConfig{
		event: []HookConfig{{Match: "bash", Command: command, Timeout: int(realSpawnTimeout / time.Millisecond)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestLoadTrustGating(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	writeSettings(t, proj, sampleSettings)
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo g"}]}}`)

	// Untrusted: only the global hook loads.
	got := Load(LoadOptions{ProjectRoot: proj, HomeDir: home, Trusted: false})
	if len(got) != 1 || got[0].Scope != ScopeGlobal {
		t.Fatalf("untrusted load should be global-only, got %d %+v", len(got), got)
	}
	// Trusted: project hooks (before global) load too.
	got = Load(LoadOptions{ProjectRoot: proj, HomeDir: home, Trusted: true})
	if len(got) != 3 {
		t.Fatalf("trusted load should include project + global, got %d", len(got))
	}
	if got[0].Scope != ScopeProject {
		t.Errorf("project hooks should sort first, got %s", got[0].Scope)
	}
}

func TestLoadDecodesGB18030GlobalSettings(t *testing.T) {
	home := t.TempDir()
	body := `{"hooks":{"Stop":[{"command":"echo 中文","description":"全局"}]}}`
	writeHookTestBytes(t, GlobalSettingsPath(home), fileencoding.Encode(body, fileencoding.GB18030))

	got := Load(LoadOptions{HomeDir: home})
	if len(got) != 1 {
		t.Fatalf("Load hooks = %+v, want one decoded global hook", got)
	}
	if got[0].Scope != ScopeGlobal || got[0].Event != Stop || got[0].Command != "echo 中文" || got[0].Description != "全局" {
		t.Fatalf("decoded global hook = %+v", got[0])
	}
}

func TestLoadDecodesUTF8BOMProjectSettings(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	body := `{"hooks":{"PreToolUse":[{"match":"bash","command":"echo pre"}]}}`
	writeHookTestBytes(t, ProjectSettingsPath(proj), fileencoding.Encode(body, fileencoding.UTF8BOM))

	got := Load(LoadOptions{HomeDir: home, ProjectRoot: proj, Trusted: true})
	if len(got) != 1 {
		t.Fatalf("Load hooks = %+v, want one decoded project hook", got)
	}
	if got[0].Scope != ScopeProject || got[0].Event != PreToolUse || got[0].Match != "bash" || got[0].Command != "echo pre" {
		t.Fatalf("decoded project hook = %+v", got[0])
	}
}

func TestLoadNormalizesQuotedNodeEvalHooksPerProject(t *testing.T) {
	requireNode(t)

	home := t.TempDir()
	projA := t.TempDir()
	projB := t.TempDir()
	script := "const payload = JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log(payload.toolName)"
	bad := `node -e "\"` + script + `\""`
	want := NormalizeCommand(bad)
	if want == bad {
		t.Fatal("test command did not normalize")
	}
	writeSettings(t, projA, hookSettingsWithCommand(t, PreToolUse, bad))
	writeSettings(t, projB, hookSettingsWithCommand(t, PreToolUse, bad))

	for _, project := range []string{projA, projB, projB} {
		hooks := Load(LoadOptions{HomeDir: home, ProjectRoot: project, Trusted: true})
		if len(hooks) != 1 {
			t.Fatalf("Load(%q) hooks = %+v, want one", project, hooks)
		}
		if hooks[0].Command != want {
			t.Fatalf("Load(%q) command = %q, want %q", project, hooks[0].Command, want)
		}
		rep := Run(context.Background(), Payload{Event: PreToolUse, Cwd: project, ToolName: "bash"}, hooks, nil)
		if len(rep.Outcomes) != 1 || rep.Outcomes[0].Decision != DecisionPass || rep.Outcomes[0].Stdout != "bash" {
			t.Fatalf("normalized hook outcome = %+v, want pass with bash stdout", rep)
		}
	}
}

func TestNormalizeCommandRepairsOnlyStdinNodeEvalQuoting(t *testing.T) {
	script := "const payload = JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log(payload.toolName)"
	doubleQuoteScript := `const payload = JSON.parse(require(\"fs\").readFileSync(0, \"utf8\")); console.log(payload.toolName)`
	tests := []struct {
		name    string
		command string
		repair  bool
	}{
		{
			name:    "quoted script argument",
			command: `node -e "\"` + script + `\""`,
			repair:  true,
		},
		{
			name:    "json escaped shell quotes",
			command: `node -e \"` + script + `\"`,
			repair:  true,
		},
		{
			name:    "json escaped shell and script quotes",
			command: `node -e \"` + doubleQuoteScript + `\"`,
			repair:  true,
		},
		{
			name:    "normal hook command",
			command: `node -e "` + script + `"`,
		},
		{
			name:    "intentional string literal",
			command: `node -e '"hello"'`,
		},
		{
			name:    "not stdin hook script",
			command: `node -e "\"console.log(1)\""`,
		},
		{
			name:    "compound command",
			command: `node -e "\"` + script + `\"" && echo done`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeCommand(tt.command)
			if tt.repair {
				if got == tt.command {
					t.Fatalf("NormalizeCommand(%q) did not repair", tt.command)
				}
				if strings.Contains(got, `\""`) {
					t.Fatalf("NormalizeCommand(%q) left accidental escaped quotes in %q", tt.command, got)
				}
				requireNode(t)
				r := DefaultSpawner(context.Background(), SpawnInput{
					Command: got,
					Stdin:   `{"toolName":"bash"}`,
					Timeout: realSpawnTimeout,
				})
				if r.ExitCode != 0 || r.Stdout != "bash" {
					t.Fatalf("normalized command did not execute: command=%q result=%+v", got, r)
				}
				return
			}
			if got != tt.command {
				t.Fatalf("NormalizeCommand(%q) = %q, want unchanged", tt.command, got)
			}
		})
	}
}

func TestNormalizeCommandRepairsOnlyPowerShellFileEscapedQuotes(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "powershell file path copied with json escaped quotes",
			command: `powershell -File \"C:\Users\Example\.reasonix\hooks\archive-attachments.ps1\"`,
			want:    `powershell -File "C:\Users\Example\.reasonix\hooks\archive-attachments.ps1"`,
		},
		{
			name:    "pwsh file path with spaces",
			command: `pwsh.exe -NoProfile -NonInteractive -File \"C:\Program Files\Reasonix Hooks\archive attachments.ps1\"`,
			want:    `pwsh.exe -NoProfile -NonInteractive -File "C:\Program Files\Reasonix Hooks\archive attachments.ps1"`,
		},
		{
			name:    "doubly escaped copied quotes",
			command: `pwsh -File \\\"C:\Program Files\Reasonix Hooks\archive attachments.ps1\\\" \"arg with spaces\"`,
			want:    `pwsh -File "C:\Program Files\Reasonix Hooks\archive attachments.ps1" "arg with spaces"`,
		},
		{
			name:    "powershell executable path copied with escaped quotes",
			command: `\"C:\Program Files\PowerShell\7\pwsh.exe\" -File \"C:\hooks\archive.ps1\"`,
			want:    `"C:\Program Files\PowerShell\7\pwsh.exe" -File "C:\hooks\archive.ps1"`,
		},
		{
			name:    "well formed file command stays unchanged",
			command: `powershell -NoProfile -File "C:\Program Files\Reasonix Hooks\archive attachments.ps1"`,
			want:    `powershell -NoProfile -File "C:\Program Files\Reasonix Hooks\archive attachments.ps1"`,
		},
		{
			name:    "command mode may intentionally contain escaped quotes",
			command: `powershell -Command \"Write-Output hi\"`,
			want:    `powershell -Command \"Write-Output hi\"`,
		},
		{
			name:    "compound command is left alone",
			command: `powershell -File \"C:\hooks\archive.ps1\" && echo done`,
			want:    `powershell -File \"C:\hooks\archive.ps1\" && echo done`,
		},
		{
			name:    "multiline command is left alone",
			command: "powershell -File \\\"C:\\hooks\\archive.ps1\\\"\necho done",
			want:    "powershell -File \\\"C:\\hooks\\archive.ps1\\\"\necho done",
		},
		{
			name:    "well formed sibling argument keeps its escaped quotes",
			command: `powershell -File \"C:\hooks\archive.ps1\" "say \"hi\""`,
			want:    `powershell -File "C:\hooks\archive.ps1" "say \"hi\""`,
		},
		{
			name:    "single quoted sibling argument stays literal",
			command: `powershell -File \"C:\hooks\archive.ps1\" 'keep \" literal'`,
			want:    `powershell -File "C:\hooks\archive.ps1" 'keep \" literal'`,
		},
		{
			name:    "non powershell command is left alone",
			command: `python \"C:\hooks\archive.py\"`,
			want:    `python \"C:\hooks\archive.py\"`,
		},
		{
			name:    "missing file argument is left alone",
			command: `powershell -NoProfile -File`,
			want:    `powershell -NoProfile -File`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCommand(tt.command); got != tt.want {
				t.Fatalf("NormalizeCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestLoadNormalizesPowerShellFileEscapedQuotes(t *testing.T) {
	home := t.TempDir()
	bad := `powershell -File \"C:\Program Files\Reasonix Hooks\archive attachments.ps1\"`
	want := `powershell -File "C:\Program Files\Reasonix Hooks\archive attachments.ps1"`
	writeSettings(t, home, hookSettingsWithCommand(t, SessionStart, bad))

	hooks := Load(LoadOptions{HomeDir: home})
	if len(hooks) != 1 {
		t.Fatalf("Load hooks = %+v, want one", hooks)
	}
	if hooks[0].Command != want {
		t.Fatalf("loaded command = %q, want %q", hooks[0].Command, want)
	}
}

func TestRepairablePowerShellFileArgs(t *testing.T) {
	command := `powershell -NoProfile -NonInteractive -File \"C:\Program Files\Reasonix Hooks\archive attachments.ps1\" -Mode \"startup\"`
	name, args, ok := repairablePowerShellFileArgs(command)
	if !ok {
		t.Fatalf("repairablePowerShellFileArgs(%q) ok = false, want true", command)
	}
	if name != "powershell" {
		t.Fatalf("name = %q, want powershell", name)
	}
	wantArgs := []string{"-NoProfile", "-NonInteractive", "-File", `C:\Program Files\Reasonix Hooks\archive attachments.ps1`, "-Mode", "startup"}
	if strings.Join(args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("args = %#v, want %#v", args, wantArgs)
	}
	if _, _, ok := repairablePowerShellFileArgs(`powershell -File "C:\hooks\archive.ps1"`); ok {
		t.Fatal("well formed PowerShell command should keep shell execution")
	}
	if _, _, ok := repairablePowerShellFileArgs(`powershell -File \"C:\hooks\archive.ps1\" && echo done`); ok {
		t.Fatal("compound PowerShell command should not be direct-exec repaired")
	}
	if _, _, ok := repairablePowerShellFileArgs("powershell -File \\\"C:\\hooks\\archive.ps1\\\"\necho done"); ok {
		t.Fatal("multiline PowerShell command should not be direct-exec repaired")
	}
}

func TestLoadPermissionRequestHook(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{"hooks":{"PermissionRequest":[{"match":"bash","command":"notify"}]}}`)

	got := Load(LoadOptions{HomeDir: home})
	if len(got) != 1 {
		t.Fatalf("hooks count = %d, want 1", len(got))
	}
	if got[0].Event != PermissionRequest || got[0].Match != "bash" || got[0].Command != "notify" {
		t.Fatalf("loaded hook = %+v, want PermissionRequest/bash/notify", got[0])
	}
}

func TestLoadIncludesPluginSessionStartHook(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "superpowers")
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo global"}]}}`)
	writeHookTestFile(t, filepath.Join(root, pluginpkg.CodexManifest), `{
  "name": "superpowers",
  "version": "6.1.0",
  "skills": "./skills/"
}`)
	writeHookTestFile(t, filepath.Join(root, "hooks", "session-start-codex"), "#!/usr/bin/env bash\necho ok\n")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "6.1.0",
		ManifestKind: "codex",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	got := Load(LoadOptions{HomeDir: home, ProjectRoot: "/workspace", Trusted: true})
	if len(got) != 2 {
		t.Fatalf("hooks = %+v, want plugin + global", got)
	}
	if got[0].Scope != ScopePlugin || got[0].Event != SessionStart {
		t.Fatalf("first hook = %+v, want plugin SessionStart", got[0])
	}
	if got[0].Env["REASONIX_PLUGIN_NAME"] != "superpowers" || got[0].Env["REASONIX_WORKSPACE_ROOT"] != "/workspace" {
		t.Fatalf("plugin env = %#v", got[0].Env)
	}
	if got[1].Scope != ScopeGlobal {
		t.Fatalf("second hook = %+v, want global", got[1])
	}
}

func TestLoadIncludesPluginClaudeCompatibilityHooks(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "claude-pack")
	writeHookTestFile(t, filepath.Join(root, pluginpkg.CodexManifest), `{
  "name": "claude-pack",
  "version": "1.0.0",
  "skills": "skills"
}`)
	writeHookTestFile(t, filepath.Join(root, "CLAUDE.md"), "Use the bundled workflow.")
	writeHookTestFile(t, filepath.Join(root, ".claude", "settings.json"), `{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "bash",
        "hooks": [
          { "type": "command", "command": "node hooks/post-tool.js", "timeout": 2 }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          { "type": "command", "command": "node hooks/prompt.js" }
        ]
      }
    ]
  }
}`)
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "claude-pack",
		Root:         "plugins/claude-pack",
		Version:      "1.0.0",
		ManifestKind: "codex",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	got := Load(LoadOptions{HomeDir: home, ProjectRoot: "/workspace", Trusted: true})
	if len(got) != 3 {
		t.Fatalf("hooks = %+v, want three plugin hooks", got)
	}
	byEvent := map[Event]ResolvedHook{}
	for _, h := range got {
		if h.Scope != ScopePlugin {
			t.Fatalf("hook scope = %s, want plugin: %+v", h.Scope, h)
		}
		byEvent[h.Event] = h
	}
	if h := byEvent[SessionStart]; h.ContextFile != filepath.Join(root, "CLAUDE.md") || h.Command != "" {
		t.Fatalf("SessionStart hook = %+v, want CLAUDE.md context file", h)
	}
	if h := byEvent[PostToolUse]; h.Match != "bash" || h.Command != "node hooks/post-tool.js" || h.Timeout != 2000 || h.Cwd != root {
		t.Fatalf("PostToolUse hook = %+v", h)
	}
	if h := byEvent[UserPromptSubmit]; h.Command != "node hooks/prompt.js" || h.Cwd != root {
		t.Fatalf("UserPromptSubmit hook = %+v", h)
	}
	if h := byEvent[PostToolUse]; h.Env["CLAUDE_PROJECT_DIR"] != "/workspace" || h.Env["REASONIX_PLUGIN_NAME"] != "claude-pack" {
		t.Fatalf("plugin env = %#v", h.Env)
	}
	if h := byEvent[PostToolUse]; h.PayloadFormat != "claude" || h.Env["CLAUDE_PLUGIN_ROOT"] != root {
		t.Fatalf("Claude compatibility metadata = %+v", h)
	}
}

func TestLoadExpandsReasonixPluginRootBeforeShellLaunch(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "impeccable")
	projectRoot := filepath.Join(home, "$CLAUDE_PLUGIN_ROOT-project")
	writeHookTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{
  "name": "impeccable",
  "version": "3.9.1",
  "hooks": {
    "PostToolUse": [{
      "match": "edit",
      "command": "node \"${REASONIX_PLUGIN_ROOT}/skills/impeccable/scripts/hook.mjs\"",
      "shellCommand": true,
      "cwd": "${REASONIX_PLUGIN_ROOT}/work",
      "env": {"IMPECCABLE_CACHE": "%REASONIX_PLUGIN_ROOT%/cache"}
    }]
  }
}`)
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "impeccable",
		Root:         "plugins/impeccable",
		Version:      "3.9.1",
		ManifestKind: "native",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	got := Load(LoadOptions{HomeDir: home, ProjectRoot: projectRoot, Trusted: true})
	if len(got) != 1 {
		t.Fatalf("hooks = %+v, want one plugin hook", got)
	}
	want := `node "` + root + `/skills/impeccable/scripts/hook.mjs"`
	if got[0].Command != want {
		t.Fatalf("plugin command = %q, want %q", got[0].Command, want)
	}
	if strings.Contains(got[0].Command, "PLUGIN_ROOT") {
		t.Fatalf("plugin root token reached the shell: %q", got[0].Command)
	}
	if got[0].Cwd != filepath.Join(root, "work") || got[0].Env["IMPECCABLE_CACHE"] != root+"/cache" {
		t.Fatalf("expanded plugin cwd/env = cwd %q env %#v", got[0].Cwd, got[0].Env)
	}
	if got[0].Env["CLAUDE_PROJECT_DIR"] != projectRoot || got[0].Env["REASONIX_WORKSPACE_ROOT"] != projectRoot {
		t.Fatalf("host-provided workspace paths were expanded: %#v", got[0].Env)
	}
}

func TestExpandPluginRootSupportsClaudeReasonixAndCmdAliases(t *testing.T) {
	root := `C:\Program Files\Reasonix\plugins\impeccable`
	for _, token := range []string{
		"${CLAUDE_PLUGIN_ROOT}", "$CLAUDE_PLUGIN_ROOT", "%CLAUDE_PLUGIN_ROOT%",
		"${REASONIX_PLUGIN_ROOT}", "$REASONIX_PLUGIN_ROOT", "%REASONIX_PLUGIN_ROOT%",
	} {
		t.Run(token, func(t *testing.T) {
			command := `node "` + token + `/skills/impeccable/scripts/hook.mjs"`
			want := `node "` + root + `/skills/impeccable/scripts/hook.mjs"`
			if got := expandPluginRoot(command, root); got != want {
				t.Fatalf("expandPluginRoot(%q) = %q, want %q", token, got, want)
			}
		})
	}
	// Shell parameter expressions belong to an explicitly requested POSIX
	// shell and must stay intact for that shell to evaluate.
	guard := `${CLAUDE_PLUGIN_ROOT:-missing}`
	if got := expandPluginRoot(guard, root); got != guard {
		t.Fatalf("shell parameter expression = %q, want unchanged %q", got, guard)
	}
	for _, longerName := range []string{
		"$CLAUDE_PLUGIN_ROOT_SUFFIX",
		"$REASONIX_PLUGIN_ROOT_OLD",
		"$CLAUDE_PLUGIN_ROOT2",
	} {
		if got := expandPluginRoot(longerName, root); got != longerName {
			t.Fatalf("longer variable name %q was partially expanded to %q", longerName, got)
		}
	}
	if got, want := expandPluginRoot(`$CLAUDE_PLUGIN_ROOT-child`, root), root+"-child"; got != want {
		t.Fatalf("delimited unbraced variable = %q, want %q", got, want)
	}
	if got, want := expandPluginRoot(`$CLAUDE_PLUGIN_ROOT/$REASONIX_PLUGIN_ROOT`, root), root+"/"+root; got != want {
		t.Fatalf("both root aliases = %q, want %q", got, want)
	}
}

func TestExpandPluginRootDoesNotReprocessResolvedRoot(t *testing.T) {
	root := `/tmp/$REASONIX_PLUGIN_ROOT/%CLAUDE_PLUGIN_ROOT%/${CLAUDE_PLUGIN_ROOT}`
	value := `${CLAUDE_PLUGIN_ROOT}|$REASONIX_PLUGIN_ROOT|%CLAUDE_PLUGIN_ROOT%`
	want := root + "|" + root + "|" + root
	if got := expandPluginRoot(value, root); got != want {
		t.Fatalf("resolved root was expanded again: got %q, want %q", got, want)
	}
}

func TestWindowsPOSIXShellInvocationPreservesQuotedScript(t *testing.T) {
	command := `sh -c '[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] || { echo "plugin root missing" >&2; exit 1; }'`
	wantShell := `C:\Program Files\Git\bin\bash.exe`
	gotShell, gotArgs, matched, err := windowsPOSIXShellInvocationWith(command, func() (string, error) {
		return wantShell, nil
	})
	if err != nil || !matched {
		t.Fatalf("explicit sh command matched=%v err=%v", matched, err)
	}
	if gotShell != wantShell {
		t.Fatalf("shell = %q, want %q", gotShell, wantShell)
	}
	wantArgs := []string{"-c", `[ -n "${CLAUDE_PLUGIN_ROOT:-}" ] || { echo "plugin root missing" >&2; exit 1; }`}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestWindowsPOSIXShellInvocationRejectsMissingBashClearly(t *testing.T) {
	_, _, matched, err := windowsPOSIXShellInvocationWith(`bash -lc 'printf ok'`, func() (string, error) {
		return "", missingWindowsHookBashError()
	})
	if !matched || err == nil || !strings.Contains(err.Error(), "Git Bash") {
		t.Fatalf("missing Bash matched=%v err=%v", matched, err)
	}
	called := false
	_, _, matched, err = windowsPOSIXShellInvocationWith(`node hook.mjs`, func() (string, error) {
		called = true
		return "", nil
	})
	if matched || err != nil || called {
		t.Fatalf("non-shell command matched=%v err=%v resolver_called=%v", matched, err, called)
	}
}

func TestWindowsPOSIXShellExecFormUsesDiscoveredBash(t *testing.T) {
	wantShell := `C:\Program Files\Git\bin\bash.exe`
	wantArgs := []string{"-lc", `printf "%s" "$HOOK_TEST_MARKER"`}
	gotShell, gotArgs, matched, err := windowsPOSIXShellArgvInvocationWith("sh", wantArgs, func() (string, error) {
		return wantShell, nil
	})
	if err != nil || !matched || gotShell != wantShell || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("exec-form sh = shell %q args %#v matched=%v err=%v", gotShell, gotArgs, matched, err)
	}
}

func TestWindowsBatchCommandLinePreservesQuotedPluginPath(t *testing.T) {
	command := `"C:\Users\Test User\AppData\Roaming\reasonix\plugins\superpowers/hooks/run-hook.cmd" session-start`
	got, ok := windowsBatchCommandLine(command)
	if !ok {
		t.Fatal("quoted plugin batch command was not recognized")
	}
	want := `cmd.exe /d /s /c ""C:\Users\Test User\AppData\Roaming\reasonix\plugins\superpowers\hooks\run-hook.cmd" session-start"`
	if got != want {
		t.Fatalf("batch command line = %q, want %q", got, want)
	}
}

func TestWindowsBatchCommandLinePreservesArgumentText(t *testing.T) {
	command := `"C:\plugins\hook.cmd" plain "argument with spaces" caret^ escaped`
	got, ok := windowsBatchCommandLine(command)
	if !ok {
		t.Fatal("quoted batch command was not recognized")
	}
	want := `cmd.exe /d /s /c ""C:\plugins\hook.cmd" plain "argument with spaces" caret^ escaped"`
	if got != want {
		t.Fatalf("batch argument text changed: got %q, want %q", got, want)
	}
}

func TestWindowsBatchArgvCommandLineSupportsNativePluginHooks(t *testing.T) {
	got, ok := windowsBatchArgvCommandLine(
		`C:\Program Files\Reasonix\plugins\example/hooks/run-hook.cmd`,
		[]string{"session-start", "argument with spaces"},
	)
	if !ok {
		t.Fatal("native plugin batch command was not recognized")
	}
	want := `cmd.exe /d /s /c ""C:\Program Files\Reasonix\plugins\example\hooks\run-hook.cmd" session-start "argument with spaces""`
	if got != want {
		t.Fatalf("batch argv command line = %q, want %q", got, want)
	}
}

func TestWindowsBatchArgvCommandLineRejectsCmdExpansionSyntax(t *testing.T) {
	for _, arg := range []string{`%PATH%`, `!HOOK_MODE!`, `embedded"quote`} {
		if got, ok := windowsBatchArgvCommandLine(`C:\plugins\hook.cmd`, []string{arg}); ok {
			t.Errorf("argv argument %q unexpectedly matched as %q", arg, got)
		}
	}
}

func TestWindowsBatchCommandLineLeavesOtherShellContractsAlone(t *testing.T) {
	commands := []string{
		`"C:\plugins\hook.cmd" session-start && echo chained`,
		`C:\plugins\hook.cmd session-start`,
		`powershell -File "C:\plugins\hook.ps1"`,
		`node "C:\plugins\hook.js"`,
		`echo hook.cmd`,
	}
	for _, command := range commands {
		if got, ok := windowsBatchCommandLine(command); ok {
			t.Errorf("windowsBatchCommandLine(%q) unexpectedly matched as %q", command, got)
		}
	}
}

func TestWindowsPOSIXShellPreservesExplicitInterpreterPaths(t *testing.T) {
	called := false
	resolve := func() (string, error) {
		called = true
		return `C:\Program Files\Git\bin\bash.exe`, nil
	}
	command := `"C:\Custom MSYS2\usr\bin\bash.exe" -c 'printf ok'`
	if _, _, matched, err := windowsPOSIXShellInvocationWith(command, resolve); matched || err != nil || called {
		t.Fatalf("explicit shell command matched=%v err=%v resolver_called=%v", matched, err, called)
	}
	if _, _, matched, err := windowsPOSIXShellArgvInvocationWith(`C:\Custom\bin\bash.exe`, []string{"-c", "printf ok"}, resolve); matched || err != nil || called {
		t.Fatalf("explicit shell argv matched=%v err=%v resolver_called=%v", matched, err, called)
	}
}

func TestHasCommandStringFlagParsesBashOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short", args: []string{"-c", "printf ok"}, want: true},
		{name: "short cluster", args: []string{"-lc", "printf ok"}, want: true},
		{name: "long option containing c", args: []string{"--norc", "script.sh"}, want: false},
		{name: "inline set option operand", args: []string{"-oc", "script.sh"}, want: false},
		{name: "shopt before command", args: []string{"-O", "extglob", "-c", "printf ok"}, want: true},
		{name: "set option before command", args: []string{"-o", "pipefail", "-c", "printf ok"}, want: true},
		{name: "rcfile before command", args: []string{"--rcfile", "custom.bashrc", "-c", "printf ok"}, want: true},
		{name: "option terminator", args: []string{"--", "-c", "printf ok"}, want: false},
		{name: "missing command string", args: []string{"-c"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasCommandStringFlag(tt.args); got != tt.want {
				t.Fatalf("hasCommandStringFlag(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestDefaultSpawnerUsesGitBashForExplicitShOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("exercises Git for Windows Bash discovery")
	}
	r := DefaultSpawner(context.Background(), SpawnInput{
		Command: `sh -c 'printf "%s" "$HOOK_TEST_MARKER"'`,
		Timeout: realSpawnTimeout,
		Env:     map[string]string{"HOOK_TEST_MARKER": "git-bash-ok"},
	})
	if r.ExitCode != 0 || r.Stdout != "git-bash-ok" {
		t.Fatalf("explicit sh hook did not run through Git Bash: %+v", r)
	}
}

func TestDecodeHookOutputRecoversGB18030WindowsErrors(t *testing.T) {
	want := `'sh' 不是内部或外部命令，也不是可运行的程序`
	raw := fileencoding.Encode(want, fileencoding.GB18030)
	if got := decodeHookOutput(raw, false); got != want {
		t.Fatalf("decoded hook stderr = %q, want %q", got, want)
	}
	utf8Text := "Error: Cannot find module 'hook.mjs'\nNode.js v24"
	if got := decodeHookOutput([]byte(utf8Text), false); got != utf8Text {
		t.Fatalf("UTF-8 hook stderr changed: %q", got)
	}
}

func TestDecodeHookOutputPreservesTruncatedUTF8Prefix(t *testing.T) {
	raw := []byte("中文")[:5]
	if got, want := decodeHookOutput(raw, true), "中"; got != want {
		t.Fatalf("truncated UTF-8 output = %q, want %q", got, want)
	}
}

func TestReasonixHomeOverridesGlobalHookPaths(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(t.TempDir(), "rx-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", reasonixHome)
	if err := os.MkdirAll(reasonixHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reasonixHome, SettingsFilename), []byte(`{"hooks":{"PostToolUse":[{"command":"echo rx"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo old"}]}}`)

	if got := GlobalSettingsPath(""); got != filepath.Join(reasonixHome, SettingsFilename) {
		t.Fatalf("GlobalSettingsPath = %q, want Reasonix home", got)
	}
	if got := TrustPath(""); got != filepath.Join(reasonixHome, TrustFilename) {
		t.Fatalf("TrustPath = %q, want Reasonix home", got)
	}
	hooks := Load(LoadOptions{})
	if len(hooks) != 1 || hooks[0].Command != "echo rx" {
		t.Fatalf("Load hooks = %+v, want Reasonix home hook only", hooks)
	}
}

func TestReasonixHomeDoesNotFallBackToLegacyWhenIsolated(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(t.TempDir(), "rx-home")
	proj := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", reasonixHome)
	writeSettings(t, home, `{"hooks":{"PostToolUse":[{"command":"echo old"}]}}`)

	hooks := Load(LoadOptions{})
	if len(hooks) != 0 {
		t.Fatalf("Load hooks = %+v, want empty (isolated REASONIX_HOME must not load legacy hooks)", hooks)
	}

	absProj, err := filepath.Abs(proj)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(trustFile{Projects: map[string]bool{absProj: true}})
	if err != nil {
		t.Fatal(err)
	}
	legacyTrust := filepath.Join(home, SettingsDirname, TrustFilename)
	if err := os.WriteFile(legacyTrust, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if IsTrusted(proj, "") {
		t.Fatal("legacy trust must not be honored when REASONIX_HOME is set and trust.json is absent")
	}
	if err := Trust(proj, ""); err != nil {
		t.Fatalf("Trust: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reasonixHome, TrustFilename)); err != nil {
		t.Fatalf("Trust should write current Reasonix home trust file: %v", err)
	}
}

func TestProjectDefinesHooks(t *testing.T) {
	proj := t.TempDir()
	if ProjectDefinesHooks(proj) {
		t.Error("empty project should define no hooks")
	}
	writeSettings(t, proj, sampleSettings)
	if !ProjectDefinesHooks(proj) {
		t.Error("project with settings.json should define hooks")
	}
}

func TestMalformedSettingsIgnored(t *testing.T) {
	home := t.TempDir()
	writeSettings(t, home, `{not valid json`)
	if got := Load(LoadOptions{HomeDir: home}); len(got) != 0 {
		t.Errorf("malformed settings should yield no hooks, got %d", len(got))
	}
}

func TestMatchesTool(t *testing.T) {
	pre := func(match string) ResolvedHook {
		return ResolvedHook{HookConfig: HookConfig{Match: match}, Event: PreToolUse}
	}
	if MatchesTool(pre("file"), "read_file") {
		t.Error(`anchored "file" must not match "read_file"`)
	}
	if !MatchesTool(pre(".*file"), "read_file") {
		t.Error(`".*file" should match "read_file"`)
	}
	if !MatchesTool(pre("bash"), "bash") {
		t.Error(`"bash" should match "bash"`)
	}
	if !MatchesTool(pre("*"), "anything") || !MatchesTool(pre(""), "anything") {
		t.Error(`"*"/"" should match every tool`)
	}
	if MatchesTool(pre("["), "bash") {
		t.Error("malformed regex should not fire")
	}
	perm := func(match string) ResolvedHook {
		return ResolvedHook{HookConfig: HookConfig{Match: match}, Event: PermissionRequest}
	}
	if !MatchesTool(perm("bash"), "bash") {
		t.Error(`PermissionRequest "bash" should match "bash"`)
	}
	if MatchesTool(perm("bash"), "read_file") {
		t.Error(`PermissionRequest "bash" must not match "read_file"`)
	}
	if MatchesTool(perm("["), "bash") {
		t.Error("malformed PermissionRequest regex should not fire")
	}
	// Non-tool events always match regardless of the match field.
	prompt := ResolvedHook{HookConfig: HookConfig{Match: "bash"}, Event: UserPromptSubmit}
	if !MatchesTool(prompt, "") {
		t.Error("non-tool events should always match")
	}
}

func TestMatchesToolTranslatesClaudeToolNames(t *testing.T) {
	claude := func(match string) ResolvedHook {
		return ResolvedHook{HookConfig: HookConfig{Match: match, PayloadFormat: "claude"}, Event: PreToolUse}
	}
	if !MatchesTool(claude("Bash"), "bash") {
		t.Error(`Claude matcher "Bash" should match Reasonix tool "bash"`)
	}
	if !MatchesTool(claude("Write|Edit"), "write_file") {
		t.Error(`Claude matcher "Write|Edit" should match Reasonix tool "write_file"`)
	}
	if !MatchesTool(claude("Write|Edit"), "edit_file") {
		t.Error(`Claude matcher "Write|Edit" should match Reasonix tool "edit_file"`)
	}
	if MatchesTool(claude("Bash"), "write_file") {
		t.Error(`Claude matcher "Bash" must not match Reasonix tool "write_file"`)
	}
	// A native (non-Claude) hook's matcher stays in Reasonix's own vocabulary.
	native := ResolvedHook{HookConfig: HookConfig{Match: "bash"}, Event: PreToolUse}
	if MatchesTool(native, "Bash") {
		t.Error("native hook matcher must not be interpreted against Claude tool names")
	}
	// The subagent tool was renamed "Task" -> "Agent" by Claude; a matcher
	// using either name must still fire against Reasonix's "task" tool.
	if !MatchesTool(claude("Agent"), "task") {
		t.Error(`Claude matcher "Agent" (current name) should match Reasonix tool "task"`)
	}
	if !MatchesTool(claude("Task"), "task") {
		t.Error(`Claude matcher "Task" (legacy alias) should still match Reasonix tool "task"`)
	}
	if !MatchesTool(claude("AskUserQuestion"), "ask") {
		t.Error(`Claude matcher "AskUserQuestion" should match Reasonix tool "ask"`)
	}
	for _, name := range []string{"bash_output", "wait"} {
		if !MatchesTool(claude("TaskOutput"), name) || !MatchesTool(claude("BashOutput"), name) {
			t.Errorf(`current "TaskOutput" and legacy "BashOutput" matchers should match Reasonix tool %q`, name)
		}
	}
	if !MatchesTool(claude("TaskStop"), "kill_shell") || !MatchesTool(claude("KillShell"), "kill_shell") {
		t.Error(`current "TaskStop" and legacy "KillShell" matchers should match Reasonix tool "kill_shell"`)
	}
}

func TestClaudeFacingToolNameUsesCurrentNames(t *testing.T) {
	if got := claudeFacingToolName("task"); got != "Agent" {
		t.Errorf(`claudeFacingToolName("task") = %q, want "Agent" (current Claude tool name)`, got)
	}
	if got := claudeFacingToolName("ask"); got != "AskUserQuestion" {
		t.Errorf(`claudeFacingToolName("ask") = %q, want "AskUserQuestion"`, got)
	}
	if got := claudeFacingToolName("run_skill"); got != "Skill" {
		t.Errorf(`claudeFacingToolName("run_skill") = %q, want "Skill"`, got)
	}
	if got := claudeFacingToolName("read_only_skill"); got != "Skill" {
		t.Errorf(`claudeFacingToolName("read_only_skill") = %q, want "Skill"`, got)
	}
	if got := claudeFacingToolName("bash_output"); got != "TaskOutput" {
		t.Errorf(`claudeFacingToolName("bash_output") = %q, want "TaskOutput"`, got)
	}
	if got := claudeFacingToolName("kill_shell"); got != "TaskStop" {
		t.Errorf(`claudeFacingToolName("kill_shell") = %q, want "TaskStop"`, got)
	}
	if got := claudeFacingToolName("wait"); got != "TaskOutput" {
		t.Errorf(`claudeFacingToolName("wait") = %q, want "TaskOutput"`, got)
	}
	// Every subagent-spawning entry point — not just "task" — corresponds to
	// Claude's single "Agent" tool, and a matcher can still use the legacy
	// "Task" name.
	for _, name := range []string{"task", "read_only_task", "parallel_tasks", "explore", "research", "review", "security_review"} {
		if got := claudeFacingToolName(name); got != "Agent" {
			t.Errorf(`claudeFacingToolName(%q) = %q, want "Agent"`, name, got)
		}
		claude := ResolvedHook{HookConfig: HookConfig{Match: "Agent", PayloadFormat: "claude"}, Event: PreToolUse}
		if !MatchesTool(claude, name) {
			t.Errorf(`Claude matcher "Agent" should match Reasonix tool %q`, name)
		}
		legacy := ResolvedHook{HookConfig: HookConfig{Match: "Task", PayloadFormat: "claude"}, Event: PreToolUse}
		if !MatchesTool(legacy, name) {
			t.Errorf(`legacy Claude matcher "Task" should still match Reasonix tool %q`, name)
		}
	}
}

func TestClaudeFacingToolInputAdaptsMappedTools(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		args     string
		want     string
	}{
		{"write_file", "write_file", `{"path":"a.txt","content":"hi"}`, `{"content":"hi","file_path":"a.txt"}`},
		{"edit_file", "edit_file", `{"path":"a.txt","old_string":"x","new_string":"y"}`, `{"file_path":"a.txt","new_string":"y","old_string":"x"}`},
		{"read_file", "read_file", `{"path":"a.txt"}`, `{"file_path":"a.txt"}`},
		{"multi_edit", "multi_edit", `{"path":"a.txt","edits":[]}`, `{"edits":[],"file_path":"a.txt"}`},
		{"notebook_edit", "notebook_edit", `{"path":"nb.ipynb","cell_id":"c1","new_source":"x"}`, `{"notebook_path":"nb.ipynb","cell_id":"c1","new_source":"x"}`},
		{"notebook-edit-delete-default-source", "notebook_edit", `{"path":"nb.ipynb","cell_number":2,"edit_mode":"delete"}`, `{"notebook_path":"nb.ipynb","cell_number":2,"edit_mode":"delete","new_source":""}`},
		{"notebook-edit-source-alias", "notebook_edit", `{"path":"nb.ipynb","cell_id":"c1","content":"x"}`, `{"notebook_path":"nb.ipynb","cell_id":"c1","content":"x","new_source":"x"}`},
		{"run_skill", "run_skill", `{"name":"deploy","arguments":"prod"}`, `{"skill":"deploy","args":"prod"}`},
		{"read_only_skill", "read_only_skill", `{"name":"explore","arguments":"map the auth flow"}`, `{"skill":"explore","args":"map the auth flow"}`},
		{"task-output", "bash_output", `{"job_id":"bash-1","filter":"err"}`, `{"task_id":"bash-1","filter":"err","block":false,"timeout":0}`},
		{"task-output-wait-one", "wait", `{"job_ids":["task-1"],"timeout_seconds":3}`, `{"job_ids":["task-1"],"timeout_seconds":3,"task_id":"task-1","block":true,"timeout":3000}`},
		{"task-output-wait-many", "wait", `{"job_ids":["task-1","task-2"]}`, `{"job_ids":["task-1","task-2"],"block":true}`},
		{"task-stop", "kill_shell", `{"job_id":"bash-1"}`, `{"task_id":"bash-1"}`},
		{"ask-defaults", "ask", `{"questions":[{"question":"Which?","header":"Choice","options":[{"label":"A"},{"label":"B","description":"Keep B"}]}]}`, `{"questions":[{"question":"Which?","header":"Choice","multiSelect":false,"options":[{"label":"A","description":""},{"label":"B","description":"Keep B"}]}]}`},
		{"todo-default-active-form", "todo_write", `{"todos":[{"content":"Run tests","status":"pending"},{"content":"Ship it","status":"completed","activeForm":"Shipping it"}]}`, `{"todos":[{"content":"Run tests","status":"pending","activeForm":"Run tests"},{"content":"Ship it","status":"completed","activeForm":"Shipping it"}]}`},
		{"task-default-description", "task", `{"prompt":"do it"}`, `{"prompt":"do it","description":"Run delegated subagent task"}`},
		{"task-explicit-description", "task", `{"prompt":"do it","description":"Inspect the auth flow"}`, `{"prompt":"do it","description":"Inspect the auth flow"}`},
		{"read-only-task-default-description", "read_only_task", `{"prompt":"inspect it"}`, `{"prompt":"inspect it","description":"Run read-only research task"}`},
		{"explore-wrapper", "explore", `{"task":"find all callers of X"}`, `{"prompt":"find all callers of X","description":"Explore the codebase"}`},
		{"research-wrapper", "research", `{"task":"compare the SDK"}`, `{"prompt":"compare the SDK","description":"Research external references"}`},
		{"review-wrapper", "review", `{"task":"review the diff"}`, `{"prompt":"review the diff","description":"Review the current changes"}`},
		{"security-review-wrapper", "security_review", `{"task":"audit the diff"}`, `{"prompt":"audit the diff","description":"Review security risks"}`},
		{"web_fetch-unchanged", "web_fetch", `{"url":"https://example.com"}`, `{"url":"https://example.com"}`},
		{"bash-unchanged", "bash", `{"command":"ls"}`, `{"command":"ls"}`},
		{"grep-unchanged", "grep", `{"pattern":"foo","path":"."}`, `{"pattern":"foo","path":"."}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := claudeFacingToolInput(c.toolName, json.RawMessage(c.args), "")
			var gotObj, wantObj map[string]any
			if err := json.Unmarshal(got, &gotObj); err != nil {
				t.Fatalf("got invalid JSON %q: %v", got, err)
			}
			if err := json.Unmarshal([]byte(c.want), &wantObj); err != nil {
				t.Fatalf("bad test want: %v", err)
			}
			if !reflect.DeepEqual(gotObj, wantObj) {
				t.Fatalf("got = %s, want %s", got, c.want)
			}
		})
	}
}

// TestClaudeFacingToolInputResolvesAbsolutePaths checks the Claude file-tool
// contract ("file_path must be absolute"): a relative Reasonix path resolves
// against the payload cwd — the same root the tool itself resolves against —
// so a prefix-matching guard sees the path the tool actually accesses.
func TestClaudeFacingToolInputResolvesAbsolutePaths(t *testing.T) {
	cwd := t.TempDir()
	got := claudeFacingToolInput("write_file", json.RawMessage(`{"path":"secrets/.env","content":"KEY=1"}`), cwd)
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	if want := filepath.Join(cwd, "secrets", ".env"); obj["file_path"] != want {
		t.Errorf("file_path = %v, want absolute %q", obj["file_path"], want)
	}

	got = claudeFacingToolInput("notebook_edit", json.RawMessage(`{"path":"nb.ipynb","cell_id":"c1"}`), cwd)
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	if want := filepath.Join(cwd, "nb.ipynb"); obj["notebook_path"] != want {
		t.Errorf("notebook_path = %v, want absolute %q", obj["notebook_path"], want)
	}

	// An already-absolute path is honored verbatim, mirroring resolveIn.
	abs := filepath.Join(cwd, "direct.txt")
	body, _ := json.Marshal(map[string]string{"path": abs})
	got = claudeFacingToolInput("read_file", body, cwd)
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	if obj["file_path"] != abs {
		t.Errorf("file_path = %v, want untouched absolute %q", obj["file_path"], abs)
	}

	// With no cwd to resolve against, the relative path passes through.
	got = claudeFacingToolInput("read_file", json.RawMessage(`{"path":"a.txt"}`), "")
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	if obj["file_path"] != "a.txt" {
		t.Errorf("file_path = %v, want relative passthrough with empty cwd", obj["file_path"])
	}
}

// TestClaudeFacingToolInputParallelTasksSynthesizesPrompt checks the
// structural adapter: parallel_tasks maps to Claude's Agent tool, so an
// Agent-scoped guard reading .tool_input.prompt must see every sub-task's
// prompt instead of failing open on a missing field.
func TestClaudeFacingToolInputParallelTasksSynthesizesPrompt(t *testing.T) {
	args := json.RawMessage(`{"tasks":[{"prompt":"scan auth","description":"a"},{"prompt":"scan crypto"}]}`)
	got := claudeFacingToolInput("parallel_tasks", args, "")
	var obj map[string]any
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatalf("got invalid JSON %q: %v", got, err)
	}
	if obj["prompt"] != "scan auth\n\nscan crypto" {
		t.Errorf("prompt = %q, want the joined sub-task prompts", obj["prompt"])
	}
	if obj["description"] != "Run parallel subagent tasks" {
		t.Errorf("description = %q, want a stable Claude Agent description", obj["description"])
	}
	if _, kept := obj["tasks"]; !kept {
		t.Error("original tasks array should stay alongside the synthesized prompt")
	}

	// Malformed or empty tasks stay untouched rather than fabricating input.
	if got := claudeFacingToolInput("parallel_tasks", json.RawMessage(`{"tasks":[]}`), ""); string(got) != `{"tasks":[]}` {
		t.Errorf("empty tasks = %s, want passthrough", got)
	}
}

func TestClaudeFacingToolInputPassthroughEdgeCases(t *testing.T) {
	if got := claudeFacingToolInput("write_file", json.RawMessage(""), ""); string(got) != "" {
		t.Errorf("empty args = %q, want empty passthrough", got)
	}
	if got := claudeFacingToolInput("write_file", json.RawMessage("not json"), ""); string(got) != "not json" {
		t.Errorf("malformed args = %q, want unchanged passthrough", got)
	}
}

func TestDecideOutcome(t *testing.T) {
	cases := []struct {
		name   string
		event  Event
		format string
		r      SpawnResult
		want   Decision
	}{
		{"pass", PreToolUse, "", SpawnResult{ExitCode: 0}, DecisionPass},
		{"block-exit2", PreToolUse, "", SpawnResult{ExitCode: 2}, DecisionBlock},
		{"exit2-nonblocking-warns", PostToolUse, "", SpawnResult{ExitCode: 2}, DecisionWarn},
		{"permission-exit2-warns", PermissionRequest, "", SpawnResult{ExitCode: 2}, DecisionWarn},
		{"other-nonzero-warns", PreToolUse, "", SpawnResult{ExitCode: 1}, DecisionWarn},
		{"timeout-blocking", UserPromptSubmit, "", SpawnResult{TimedOut: true}, DecisionBlock},
		{"permission-timeout-warns", PermissionRequest, "", SpawnResult{TimedOut: true}, DecisionWarn},
		{"timeout-nonblocking", Stop, "", SpawnResult{TimedOut: true}, DecisionWarn},
		{"spawn-error", PreToolUse, "", SpawnResult{SpawnErr: os.ErrNotExist}, DecisionError},
		// Claude's own PermissionRequest contract blocks on exit 2/timeout the
		// same way PreToolUse does; native Reasonix PermissionRequest hooks
		// (format == "") stay advisory-only, verified above.
		{"claude-permission-exit2-blocks", PermissionRequest, "claude", SpawnResult{ExitCode: 2}, DecisionBlock},
		{"claude-permission-timeout-blocks", PermissionRequest, "claude", SpawnResult{TimedOut: true}, DecisionBlock},
	}
	for _, c := range cases {
		h := ResolvedHook{Event: c.event, HookConfig: HookConfig{PayloadFormat: c.format}}
		if got := decideOutcome(h, c.r); got != c.want {
			t.Errorf("%s: decideOutcome = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestClaudeJSONDeny(t *testing.T) {
	cases := []struct {
		name       string
		event      Event
		stdout     string
		wantDeny   bool
		wantReason string
	}{
		{
			name:       "pretooluse-permission-decision-deny",
			event:      PreToolUse,
			stdout:     `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"rm -rf blocked"}}`,
			wantDeny:   true,
			wantReason: "rm -rf blocked",
		},
		{
			name:     "pretooluse-permission-decision-allow",
			event:    PreToolUse,
			stdout:   `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`,
			wantDeny: false,
		},
		{
			name:       "permissionrequest-decision-behavior-deny",
			event:      PermissionRequest,
			stdout:     `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny"}}}`,
			wantDeny:   true,
			wantReason: "",
		},
		{
			name:     "non-json-stdout-never-denies",
			event:    PreToolUse,
			stdout:   "looks fine",
			wantDeny: false,
		},
		{
			name:     "unsupported-event-never-denies",
			event:    PostToolUse,
			stdout:   `{"hookSpecificOutput":{"hookEventName":"PostToolUse","permissionDecision":"deny"}}`,
			wantDeny: false,
		},
		{
			name:       "userpromptsubmit-top-level-decision-block",
			event:      UserPromptSubmit,
			stdout:     `{"decision":"block","reason":"prompt contains a secret"}`,
			wantDeny:   true,
			wantReason: "prompt contains a secret",
		},
		{
			name:     "userpromptsubmit-top-level-decision-approve",
			event:    UserPromptSubmit,
			stdout:   `{"decision":"approve"}`,
			wantDeny: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			deny, reason := claudeJSONDeny(c.event, c.stdout)
			if deny != c.wantDeny {
				t.Errorf("deny = %v, want %v", deny, c.wantDeny)
			}
			if reason != c.wantReason {
				t.Errorf("reason = %q, want %q", reason, c.wantReason)
			}
		})
	}
}

func TestClaudeJSONAllow(t *testing.T) {
	if !claudeJSONAllow(PermissionRequest, `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`) {
		t.Error(`PermissionRequest decision.behavior "allow" should report allow`)
	}
	if claudeJSONAllow(PermissionRequest, `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny"}}}`) {
		t.Error(`decision.behavior "deny" must not report allow`)
	}
	if claudeJSONAllow(PreToolUse, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`) {
		t.Error("only PermissionRequest carries an auto-allow decision")
	}
}

func TestParseOutputSessionStartJSONAdditionalContext(t *testing.T) {
	out, warnings := ParseOutput(SessionStart, `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"Load conventions."}}`)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if out.AdditionalContext != "Load conventions." {
		t.Fatalf("AdditionalContext = %q, want context", out.AdditionalContext)
	}
}

func TestParseOutputSessionStartPlainText(t *testing.T) {
	out, warnings := ParseOutput(SessionStart, "  Load workspace notes.  ")
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if out.AdditionalContext != "Load workspace notes." {
		t.Fatalf("AdditionalContext = %q, want plain text", out.AdditionalContext)
	}
}

func TestParseOutputRejectsMismatchedEvent(t *testing.T) {
	out, warnings := ParseOutput(SessionStart, `{"hookSpecificOutput":{"hookEventName":"Stop","additionalContext":"wrong"}}`)
	if out.AdditionalContext != "" {
		t.Fatalf("AdditionalContext = %q, want empty", out.AdditionalContext)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one warning", warnings)
	}
}

func TestParseOutputInvalidJSONWarns(t *testing.T) {
	out, warnings := ParseOutput(SessionStart, `{"hookSpecificOutput":`)
	if out.AdditionalContext != "" {
		t.Fatalf("AdditionalContext = %q, want empty", out.AdditionalContext)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one warning", warnings)
	}
}

func TestRunStopsAtFirstBlock(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "first"}, Event: PreToolUse, Scope: ScopeProject},
		{HookConfig: HookConfig{Command: "second"}, Event: PreToolUse, Scope: ScopeProject},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 2} // first blocks
	}
	rep := Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks, spawner)
	if !rep.Blocked {
		t.Error("report should be blocked")
	}
	if len(ran) != 1 || ran[0] != "first" {
		t.Errorf("should stop after the first block, ran %v", ran)
	}
}

func TestRunHonorsClaudeJSONDenyOnExitZero(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "guard", PayloadFormat: "claude"}, Event: PreToolUse},
	}
	denyJSON := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"rm -rf blocked"}}`
	rep := Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks,
		func(_ context.Context, in SpawnInput) SpawnResult { return SpawnResult{ExitCode: 0, Stdout: denyJSON} })
	if !rep.Blocked {
		t.Fatal("exit-0 hook with a Claude JSON deny decision should block")
	}
	if rep.Outcomes[0].Decision != DecisionBlock {
		t.Errorf("Decision = %s, want block", rep.Outcomes[0].Decision)
	}
}

func TestRunNativeHookIgnoresPermissionDecisionField(t *testing.T) {
	// A native (non-Claude) hook's stdout happening to contain a field named
	// "permissionDecision" must not gain new blocking power — only imported
	// Claude hooks (PayloadFormat "claude") opt into that contract.
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "guard"}, Event: PreToolUse},
	}
	denyJSON := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny"}}`
	rep := Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks,
		func(_ context.Context, in SpawnInput) SpawnResult { return SpawnResult{ExitCode: 0, Stdout: denyJSON} })
	if rep.Blocked {
		t.Fatal("native hook JSON output should not be interpreted as a Claude deny decision")
	}
}

func TestRunClaudePermissionRequestExit2Blocks(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "guard", PayloadFormat: "claude"}, Event: PermissionRequest},
	}
	rep := Run(context.Background(), Payload{Event: PermissionRequest, ToolName: "bash"}, hooks,
		func(_ context.Context, in SpawnInput) SpawnResult { return SpawnResult{ExitCode: 2} })
	if !rep.Blocked {
		t.Fatal("Claude-imported PermissionRequest hook exiting 2 should block")
	}
}

func TestRunClaudePermissionRequestJSONAllow(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "guard", PayloadFormat: "claude"}, Event: PermissionRequest},
	}
	allowJSON := `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`
	rep := Run(context.Background(), Payload{Event: PermissionRequest, ToolName: "bash"}, hooks,
		func(_ context.Context, in SpawnInput) SpawnResult { return SpawnResult{ExitCode: 0, Stdout: allowJSON} })
	if rep.Blocked {
		t.Fatal("an allow decision must not block")
	}
	if !rep.Allowed {
		t.Fatal("exit-0 hook with a Claude JSON allow decision should set Report.Allowed")
	}
}

func TestRunHonorsUserPromptSubmitTopLevelDeny(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "guard", PayloadFormat: "claude"}, Event: UserPromptSubmit},
	}
	denyJSON := `{"decision":"block","reason":"prompt contains a secret"}`
	rep := Run(context.Background(), Payload{Event: UserPromptSubmit}, hooks,
		func(_ context.Context, in SpawnInput) SpawnResult { return SpawnResult{ExitCode: 0, Stdout: denyJSON} })
	if !rep.Blocked {
		t.Fatal("exit-0 UserPromptSubmit hook with a top-level decision:block should block")
	}
}

func TestRunFiltersByEventAndTool(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "a", Match: "bash"}, Event: PreToolUse},
		{HookConfig: HookConfig{Command: "b", Match: "read_file"}, Event: PreToolUse},
		{HookConfig: HookConfig{Command: "c"}, Event: PostToolUse},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 0}
	}
	Run(context.Background(), Payload{Event: PreToolUse, ToolName: "bash"}, hooks, spawner)
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("only the matching PreToolUse hook should run, got %v", ran)
	}
}

func TestRunClaudePayloadAndDirectArgs(t *testing.T) {
	hooks := []ResolvedHook{{
		HookConfig: HookConfig{Command: "/tmp/agent-critter", Argv: []string{"--hook"}, PayloadFormat: "claude"},
		Event:      PostToolUseFailure,
	}}
	var input SpawnInput
	Run(context.Background(), Payload{
		Event: PostToolUseFailure, SessionID: "session-1", Cwd: "/workspace",
		ToolName: "bash", ToolArgs: json.RawMessage(`{"command":"false"}`),
		ToolResult: "remote: denied", Error: "exit 1",
	}, hooks, func(_ context.Context, in SpawnInput) SpawnResult { input = in; return SpawnResult{ExitCode: 0} })
	if input.Command != "/tmp/agent-critter" || len(input.Args) != 1 || input.Args[0] != "--hook" {
		t.Fatalf("direct hook input = %+v", input)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(input.Stdin), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["hook_event_name"] != string(PostToolUseFailure) || payload["session_id"] != "session-1" || payload["error"] != "exit 1" {
		t.Fatalf("Claude payload = %#v", payload)
	}
	if payload["tool_name"] != "Bash" {
		t.Fatalf("Claude payload tool_name = %v, want the Claude vocabulary name Bash for Reasonix tool bash", payload["tool_name"])
	}
	response, ok := payload["tool_response"].(map[string]any)
	if !ok || response["stdout"] != "remote: denied" || response["stderr"] != "exit 1" || response["interrupted"] != false {
		t.Fatalf("Claude tool_response = %#v, want Claude's Bash shape {stdout, stderr, interrupted}", payload["tool_response"])
	}
	if _, exists := payload["event"]; exists {
		t.Fatalf("native payload field leaked into Claude payload: %#v", payload)
	}
}

// TestRunClaudeWriteFileGuardFiresAndSeesFilePath is an end-to-end check that
// a Claude plugin's "block writes to secrets" style PreToolUse guard —
// matcher "Write", reading .tool_input.file_path — actually fires against a
// Reasonix write_file call and sees the absolute target path Claude's
// file-tool contract specifies.
func TestRunClaudeWriteFileGuardFiresAndSeesFilePath(t *testing.T) {
	cwd := t.TempDir()
	hooks := []ResolvedHook{{
		HookConfig: HookConfig{Command: "guard", Match: "Write", PayloadFormat: "claude"},
		Event:      PreToolUse,
	}}
	var input SpawnInput
	Run(context.Background(), Payload{
		Event: PreToolUse, Cwd: cwd, ToolName: "write_file",
		ToolArgs: json.RawMessage(`{"path":"secrets/.env","content":"KEY=1"}`),
	}, hooks, func(_ context.Context, in SpawnInput) SpawnResult { input = in; return SpawnResult{ExitCode: 0} })
	if input.Command == "" {
		t.Fatal(`matcher "Write" did not fire for Reasonix tool "write_file"`)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(input.Stdin), &payload); err != nil {
		t.Fatal(err)
	}
	toolInput, ok := payload["tool_input"].(map[string]any)
	if !ok {
		t.Fatalf("tool_input = %#v, want an object", payload["tool_input"])
	}
	if want := filepath.Join(cwd, "secrets", ".env"); toolInput["file_path"] != want {
		t.Fatalf(`tool_input.file_path = %v, want absolute %q (a prefix-matching guard must see the path the tool accesses)`, toolInput["file_path"], want)
	}
	if _, hasPath := toolInput["path"]; hasPath {
		t.Fatalf("tool_input still has Reasonix's \"path\" key: %#v", toolInput)
	}
}

// TestRunClaudeAgentGuardFiresAndSeesRequiredFields covers the full matcher to
// stdin path for a dedicated Reasonix subagent wrapper. Claude Agent requires
// both prompt and description even though the wrapper only accepts task.
func TestRunClaudeAgentGuardFiresAndSeesRequiredFields(t *testing.T) {
	hooks := []ResolvedHook{{
		HookConfig: HookConfig{Command: "guard", Match: "Agent", PayloadFormat: "claude"},
		Event:      PreToolUse,
	}}
	var input SpawnInput
	Run(context.Background(), Payload{
		Event: PreToolUse, ToolName: "security_review",
		ToolArgs: json.RawMessage(`{"task":"audit the auth changes"}`),
	}, hooks, func(_ context.Context, in SpawnInput) SpawnResult { input = in; return SpawnResult{ExitCode: 0} })
	if input.Command == "" {
		t.Fatal(`matcher "Agent" did not fire for Reasonix tool "security_review"`)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(input.Stdin), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_name"] != "Agent" {
		t.Fatalf("tool_name = %v, want Agent", payload["tool_name"])
	}
	toolInput, ok := payload["tool_input"].(map[string]any)
	if !ok || toolInput["prompt"] != "audit the auth changes" || toolInput["description"] != "Review security risks" {
		t.Fatalf("tool_input = %#v, want Claude Agent prompt and description", payload["tool_input"])
	}
}

func TestClaudeToolResponsePreservesPlainText(t *testing.T) {
	stdin := marshalPayload(Payload{Event: PostToolUse, ToolName: "read_file", ToolResult: "plain output"}, "claude")
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdin), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_response"] != "plain output" {
		t.Fatalf("tool_response = %#v, want plain output", payload["tool_response"])
	}
}

// TestClaudeToolResponseBashShape checks that a Bash tool_response is the
// object Claude's contract (and the official security-guidance plugin's
// commit/push checks) expect — {stdout, stderr, interrupted} — never a bare
// string, and never raw JSON even when the command's output happens to be a
// valid JSON document.
func TestClaudeToolResponseBashShape(t *testing.T) {
	stdin := marshalPayload(Payload{Event: PostToolUse, ToolName: "bash", ToolResult: `{"looks":"like json"}`}, "claude")
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdin), &payload); err != nil {
		t.Fatal(err)
	}
	response, ok := payload["tool_response"].(map[string]any)
	if !ok {
		t.Fatalf("tool_response = %#v, want an object", payload["tool_response"])
	}
	if response["stdout"] != `{"looks":"like json"}` || response["stderr"] != "" || response["interrupted"] != false {
		t.Fatalf("tool_response = %#v, want {stdout: <combined output>, stderr: \"\", interrupted: false}", response)
	}

	// An interrupted failure carries the error and the interrupt flag.
	stdin = marshalPayload(Payload{
		Event: PostToolUseFailure, ToolName: "bash",
		ToolResult: "partial", Error: "context canceled", IsInterrupt: true,
	}, "claude")
	if err := json.Unmarshal([]byte(stdin), &payload); err != nil {
		t.Fatal(err)
	}
	response, ok = payload["tool_response"].(map[string]any)
	if !ok || response["stdout"] != "partial" || response["stderr"] != "context canceled" || response["interrupted"] != true {
		t.Fatalf("failure tool_response = %#v, want {stdout, stderr, interrupted:true}", payload["tool_response"])
	}

	// PreToolUse has no result yet: no fabricated Bash response object.
	stdin = marshalPayload(Payload{Event: PreToolUse, ToolName: "bash", ToolArgs: json.RawMessage(`{"command":"ls"}`)}, "claude")
	if err := json.Unmarshal([]byte(stdin), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tool_response"] != "" {
		t.Fatalf("PreToolUse tool_response = %#v, want the empty passthrough", payload["tool_response"])
	}
}

func TestRunAsyncHookReturnsBeforeSpawnerFinishes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	hooks := []ResolvedHook{{HookConfig: HookConfig{Command: "critter", Async: true}, Event: Stop}}
	rep := Run(context.Background(), Payload{Event: Stop}, hooks, func(context.Context, SpawnInput) SpawnResult {
		close(started)
		<-release
		return SpawnResult{ExitCode: 0}
	})
	if len(rep.Outcomes) != 1 || rep.Outcomes[0].Decision != DecisionPass {
		t.Fatalf("report = %+v", rep)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("async hook did not start")
	}
	close(release)
}

func TestRunFiltersPermissionRequestByTool(t *testing.T) {
	hooks := []ResolvedHook{
		{HookConfig: HookConfig{Command: "a", Match: "bash"}, Event: PermissionRequest},
		{HookConfig: HookConfig{Command: "b", Match: "read_file"}, Event: PermissionRequest},
		{HookConfig: HookConfig{Command: "c"}, Event: Notification},
	}
	var ran []string
	spawner := func(_ context.Context, in SpawnInput) SpawnResult {
		ran = append(ran, in.Command)
		return SpawnResult{ExitCode: 0}
	}
	Run(context.Background(), Payload{Event: PermissionRequest, ToolName: "bash"}, hooks, spawner)
	if len(ran) != 1 || ran[0] != "a" {
		t.Errorf("only the matching PermissionRequest hook should run, got %v", ran)
	}
}

func TestTrustStore(t *testing.T) {
	home := t.TempDir()
	proj := t.TempDir()
	if IsTrusted(proj, home) {
		t.Error("project should start untrusted")
	}
	if err := Trust(proj, home); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if !IsTrusted(proj, home) {
		t.Error("project should be trusted after Trust")
	}
}

func TestDefaultSpawner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	ctx := context.Background()
	// exit 0 with stdout
	r := DefaultSpawner(ctx, SpawnInput{Command: "printf hi", Timeout: realSpawnTimeout})
	if r.ExitCode != 0 || r.Stdout != "hi" {
		t.Errorf("expected exit 0 / hi, got code=%d out=%q err=%v", r.ExitCode, r.Stdout, r.SpawnErr)
	}
	// exit 2 (block verdict on a gating event)
	r = DefaultSpawner(ctx, SpawnInput{Command: "exit 2", Timeout: realSpawnTimeout})
	if r.ExitCode != 2 {
		t.Errorf("expected exit 2, got %d", r.ExitCode)
	}
	// stdin is delivered as the payload
	r = DefaultSpawner(ctx, SpawnInput{Command: "cat", Stdin: "payload-here", Timeout: realSpawnTimeout})
	if r.Stdout != "payload-here" {
		t.Errorf("stdin not delivered: %q", r.Stdout)
	}
	// timeout kills the command
	r = DefaultSpawner(ctx, SpawnInput{Command: "sleep 5", Timeout: 100 * time.Millisecond})
	if !r.TimedOut {
		t.Errorf("expected timeout, got %+v", r)
	}
}

func TestDefaultSpawnerOutputCap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	// Emit more than the cap; expect truncation flagged and bounded capture.
	r := DefaultSpawner(context.Background(), SpawnInput{
		Command: "yes x | head -c 400000",
		Timeout: realSpawnTimeout,
	})
	if !r.Truncated {
		t.Error("oversized output should be flagged truncated")
	}
	if len(r.Stdout) > outputCapBytes {
		t.Errorf("captured output %d exceeds cap %d", len(r.Stdout), outputCapBytes)
	}
}

// TestWellFormedNodeEvalKeepsShellSemantics pins the execution contract for
// commands that never needed repair: hook commands are documented to run
// through the shell, and existing user hooks may rely on shell expansion.
// A well-formed node -e stdin-hook command must therefore keep $VAR expansion
// on POSIX — only repaired commands (whose broken quoting means they never
// worked through a shell) may take the direct-exec path.
func TestWellFormedNodeEvalKeepsShellSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cmd does not perform POSIX $ expansion; Windows intentionally direct-execs recognized node evals")
	}
	requireNode(t)
	command := `node -e "const payload = JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log('$HOOK_TEST_MARKER' + payload.toolName)"`
	if got := NormalizeCommand(command); got != command {
		t.Fatalf("well-formed command was rewritten: %q", got)
	}
	r := DefaultSpawner(context.Background(), SpawnInput{
		Command: command,
		Stdin:   `{"toolName":"bash"}`,
		Timeout: realSpawnTimeout,
		Env:     map[string]string{"HOOK_TEST_MARKER": "expanded-"},
	})
	if r.ExitCode != 0 {
		t.Fatalf("spawn failed: %+v", r)
	}
	if r.Stdout != "expanded-bash" {
		t.Fatalf("stdout = %q, want %q — $VAR expansion was lost (command bypassed the shell)", r.Stdout, "expanded-bash")
	}
}
