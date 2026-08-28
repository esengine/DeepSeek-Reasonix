package main

import (
	"path/filepath"
	"testing"
)

func TestTerminalCommandFromConfigIgnoresCrossKindPath(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		prefer string
		stale  string
	}{
		{"PowerShell path after selecting Bash", "bash", testExecutable(t, dir, "pwsh")},
		{"Bash path after selecting PowerShell", "powershell", testExecutable(t, dir, "bash")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command, ok := terminalCommandFromConfig(tc.prefer, tc.stale)
			if !ok {
				t.Fatal("expected the selected shell or its normal fallback to resolve")
			}
			if filepath.Clean(command.path) == filepath.Clean(tc.stale) {
				t.Fatalf("terminal reused incompatible configured path %q", command.path)
			}
		})
	}
}
