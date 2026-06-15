//go:build !windows

package control

import (
	"os/exec"
	"testing"
)

func TestSetShellKillTreeDetachesControllingTerminal(t *testing.T) {
	cmd := exec.Command("sh", "-c", "true")
	setShellKillTree(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("shell commands should run in a new session so interactive prompts cannot grab the TUI terminal")
	}
}
