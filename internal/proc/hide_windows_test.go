//go:build windows

package proc

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestHideWindowSetsCreateNoWindow(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	HideWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil; HideWindow did not set it")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CREATE_NO_WINDOW not set; CreationFlags=%#x", cmd.SysProcAttr.CreationFlags)
	}
	const detachedProcess = 0x00000008
	if cmd.SysProcAttr.CreationFlags&detachedProcess != 0 {
		t.Fatalf("DETACHED_PROCESS should not be set by HideWindow; CreationFlags=%#x", cmd.SysProcAttr.CreationFlags)
	}
}

func TestHideWindowPreservesExistingFlags(t *testing.T) {
	const createNewProcessGroup = 0x00000200
	cmd := exec.Command("cmd", "/c", "echo", "hi")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	HideWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&createNewProcessGroup == 0 {
		t.Fatal("HideWindow clobbered a pre-existing creation flag")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("HideWindow did not add CREATE_NO_WINDOW")
	}
}

func TestHideWindowPreservesStdoutCapture(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "echo", "reasonix-ok")
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	if !strings.Contains(string(out), "reasonix-ok") {
		t.Fatalf("output = %q, want it to contain reasonix-ok", out)
	}
}
