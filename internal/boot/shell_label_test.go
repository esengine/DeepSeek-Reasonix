package boot

import (
	"testing"

	"reasonix/internal/sandbox"
)

func TestResolvedShellLabelUsesBoundInterpreter(t *testing.T) {
	for _, tc := range []struct {
		name           string
		shell          sandbox.Shell
		configuredPath string
		want           string
	}{
		{"configured path uses resolved executable", sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, `C:\stale\bash.exe`, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
		{"auto resolution preserves cache-stable kind", sandbox.Shell{Kind: sandbox.ShellBash, Path: `/opt/homebrew/bin/bash`}, "", "bash"},
		{"kind fallback", sandbox.Shell{Kind: sandbox.ShellZsh}, "", "zsh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvedShellLabel(tc.shell, tc.configuredPath); got != tc.want {
				t.Fatalf("shell label = %q, want %q", got, tc.want)
			}
		})
	}
}
