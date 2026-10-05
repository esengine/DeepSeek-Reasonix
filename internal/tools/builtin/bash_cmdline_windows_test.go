//go:build windows

package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

func powerShellCommandAtUnits(t *testing.T, sh sandbox.Shell, units int) string {
	t.Helper()
	command := func(n int) string { return "Write-Output '" + strings.Repeat("中", n) + "'" }
	base, _ := commandLineUnits(unconfinedShellArgv(sh, command(0)))
	if units < base {
		t.Fatalf("target %d is below the empty command's %d units", units, base)
	}
	c := command(units - base)
	if got, _ := commandLineUnits(unconfinedShellArgv(sh, c)); got != units {
		t.Fatalf("command line is %d units, want %d", got, units)
	}
	return c
}

func TestBashRefusesCommandLineOverWindowsLimit(t *testing.T) {
	powershell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("powershell not found")
	}
	sh := sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: powershell}
	ctx := context.Background()

	fits := powerShellCommandAtUnits(t, sh, windowsCommandLineMax)
	args, _ := json.Marshal(map[string]any{"command": fits})
	res, err := (bash{shell: sh}).ExecuteDetailed(ctx, args)
	if err != nil {
		t.Fatalf("command at the limit failed: %v", err)
	}
	if !strings.Contains(res.Output, "中中中") {
		t.Fatalf("command at the limit output = %.80q", res.Output)
	}

	over := powerShellCommandAtUnits(t, sh, windowsCommandLineMax+1)
	args, _ = json.Marshal(map[string]any{"command": over})
	res, err = (bash{shell: sh}).ExecuteDetailed(ctx, args)
	if !errors.Is(err, errCommandLineTooLong) {
		t.Fatalf("command over the limit error = %v, want errCommandLineTooLong", err)
	}
	if res.Execution == nil || res.Execution.State != tool.ShellStateNotRun || res.Execution.MutationRisk != tool.ShellMutationNotStarted {
		t.Fatalf("execution = %+v, want not run and not started", res.Execution)
	}
}
