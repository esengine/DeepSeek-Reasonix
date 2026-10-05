//go:build windows

package proc

import (
	"os/exec"
	"testing"
)

func TestLowPrioritySetsBelowNormalClass(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	HideWindow(cmd)
	LowPriority(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&belowNormalPriorityClass == 0 {
		t.Fatalf("BELOW_NORMAL_PRIORITY_CLASS not set; CreationFlags=%#x", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("LowPriority must not clobber HideWindow's flags")
	}
}
