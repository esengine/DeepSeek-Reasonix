package builtin

import (
	"errors"
	"os/exec"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

func TestDeletePowerShellGoldenCauses(t *testing.T) {
	path, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell 7 unavailable")
	}
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: path}
	root := t.TempDir()
	b := bash{workDir: root, sb: sandbox.Spec{WriteRoots: []string{root}}}
	for _, tc := range []struct {
		source string
		cause  string
	}{
		{`Remove-Item -Recurse '`, CodeShellSyntax},
		{`Remove-Item -Recurse child`, ""},
		{`Remove-Item -Recurse .`, CodeDestructiveTarget},
		{`Remove-Item -Recurse *`, CodeDeleteNonliteral},
		{`Remove-Item -Recurse $target`, CodeDeleteNonliteral},
		{`$x=1; Remove-Item -Recurse child`, CodeDeleteSequence},
		{`while ($false) { Remove-Item -Recurse child }`, CodeDeleteSequence},
		{`if (Test-Path child) { Remove-Item -Recurse child }`, ""},
		{`'child' | Remove-Item -Force`, ""},
		{`Get-ChildItem child | Remove-Item -Force`, CodeDeleteNonliteral},
		{`[IO.Directory]::Delete('..',$true)`, CodeDestructiveTarget},
		{`Set-Alias z Remove-Item; z -Recurse child`, CodeDeleteSequence},
	} {
		t.Run(tc.source, func(t *testing.T) {
			err := b.refuseDestructiveDelete(t.Context(), sh, tc.source)
			if tc.cause == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var refusal tool.Refusal
			if !errors.As(err, &refusal) || refusal.Code != tc.cause {
				t.Fatalf("refusal = %v, want cause %s", err, tc.cause)
			}
		})
	}
}
