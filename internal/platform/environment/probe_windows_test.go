//go:build windows

package environment

import (
	"os/exec"
	"testing"
)

const testCreateNoWindow = 0x08000000

func TestPrepareProbeCommandHidesConsoleWindowOnWindows(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	prepareProbeCommand(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow is false")
	}
	if cmd.SysProcAttr.CreationFlags&testCreateNoWindow == 0 {
		t.Fatalf("CREATE_NO_WINDOW not set; CreationFlags=%#x", cmd.SysProcAttr.CreationFlags)
	}
}
