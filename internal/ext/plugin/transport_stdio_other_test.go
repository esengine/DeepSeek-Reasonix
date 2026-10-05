//go:build !windows

package plugin

import (
	"context"
	"os/exec"
	"testing"
)

func TestStdioShellPATHProbeDetachesControllingTerminal(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "true")
	prepareStdioShellPATHProbe(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("stdio login shell PATH probe should run in a new session so an interactive shell cannot take the TUI foreground")
	}
}
