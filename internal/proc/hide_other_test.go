//go:build !windows

package proc

import (
	"os/exec"
	"testing"
)

func TestHideWindowIsNoOpOffWindows(t *testing.T) {
	cmd := exec.Command("true")
	HideWindow(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatal("HideWindow should be a no-op off Windows, but set SysProcAttr")
	}
}
