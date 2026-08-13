package hook

import (
	"path/filepath"
	"testing"

	"reasonix/internal/pluginpkg"
)

func TestWindowsExtensionlessPOSIXPluginHookUsesBash(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin root")
	script := filepath.Join(root, "hooks", "session-start-codex")
	writeHookTestFile(t, script, "#!/usr/bin/env bash\necho ok\n")

	config := pluginHookExecutionConfigForPlatform(pluginpkg.Hook{Command: "hooks/session-start-codex"}, root, "windows")
	if config.ExecutionMode != ExecutionShell || config.Shell != "bash" {
		t.Fatalf("execution contract = mode %q shell %q, want Bash shell form", config.ExecutionMode, config.Shell)
	}
	if want := bashSingleQuote(filepath.ToSlash(script)); config.Command != want {
		t.Fatalf("command = %q, want safely quoted script path %q", config.Command, want)
	}
	if !requiresWindowsBash(config) {
		t.Fatal("extensionless POSIX plugin hook should report a Windows Bash dependency")
	}
}

func TestWindowsPluginHookDoesNotGuessBashForNativeOrNonShellFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin root")
	writeHookTestFile(t, filepath.Join(root, "hooks", "native-tool"), "MZ\x00not a shell script\n")
	writeHookTestFile(t, filepath.Join(root, "hooks", "run-hook.cmd"), "@echo off\r\n")

	for _, command := range []string{"hooks/native-tool", "hooks/run-hook.cmd"} {
		config := pluginHookExecutionConfigForPlatform(pluginpkg.Hook{Command: command}, root, "windows")
		if config.ExecutionMode != ExecutionLegacy || config.Shell != "" {
			t.Fatalf("command %q guessed shell execution: mode %q shell %q", command, config.ExecutionMode, config.Shell)
		}
	}
}

func TestWindowsImplicitPluginPOSIXScriptUsesBashPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin root")
	script := filepath.Join(root, "hooks", "start.sh")
	writeHookTestFile(t, script, "#!/usr/bin/env bash\necho ok\n")
	config := pluginHookExecutionConfigForPlatform(pluginpkg.Hook{
		Command:      `"${CLAUDE_PLUGIN_ROOT}/hooks/start.sh"`,
		ShellCommand: true,
	}, root, "windows")
	if config.ExecutionMode != ExecutionShell || config.Shell != "bash" {
		t.Fatalf("implicit shell contract = mode %q shell %q, want explicit Bash", config.ExecutionMode, config.Shell)
	}
	if want := bashSingleQuote(filepath.ToSlash(script)); config.Command != want {
		t.Fatalf("implicit shell command = %q, want %q", config.Command, want)
	}
}

func TestWindowsLegacyPOSIXScriptSpawnInputUsesBash(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace root")
	script := filepath.Join(root, "hooks", "session-start")
	writeHookTestFile(t, script, "#!/usr/bin/env bash\necho ok\n")

	got := normalizeWindowsHookSpawnInputForPlatform(SpawnInput{
		Command: "hooks/session-start",
		Mode:    ExecutionLegacy,
		Cwd:     root,
	}, "windows")
	if got.Mode != ExecutionShell || got.Shell != "bash" {
		t.Fatalf("normalized spawn contract = mode %q shell %q, want Bash shell", got.Mode, got.Shell)
	}
	if want := bashSingleQuote(filepath.ToSlash(script)); got.Command != want {
		t.Fatalf("normalized command = %q, want %q", got.Command, want)
	}
}

func TestWindowsLegacyPOSIXScriptRuntimeRequiresBash(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace root")
	script := filepath.Join(root, "hooks", "session-start")
	writeHookTestFile(t, script, "#!/usr/bin/env bash\necho ok\n")

	config := HookConfig{Command: "hooks/session-start", Cwd: root, ExecutionMode: ExecutionLegacy}
	if !requiresWindowsBash(config) {
		t.Fatal("legacy POSIX script hook should require Bash on Windows")
	}
}
