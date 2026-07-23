//go:build windows

package hook

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
	}{
		{name: "shell form", command: `"` + filepath.ToSlash(script) + `" session-start`},
		{name: "argv form", command: filepath.ToSlash(script), args: []string{"session-start"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultSpawner(context.Background(), SpawnInput{
				Command: tt.command,
				Args:    tt.args,
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
