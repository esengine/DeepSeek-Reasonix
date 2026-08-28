package boot

import (
	"testing"

	"reasonix/internal/sandbox"
)

func TestResolvedShellLabelUsesBoundInterpreter(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shell sandbox.Shell
		want  string
	}{
		{"resolved executable", sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`},
		{"kind fallback", sandbox.Shell{Kind: sandbox.ShellZsh}, "zsh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvedShellLabel(tc.shell); got != tc.want {
				t.Fatalf("shell label = %q, want %q", got, tc.want)
			}
		})
	}
}
