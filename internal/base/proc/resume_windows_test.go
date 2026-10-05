//go:build windows

package proc

import (
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// A suspended launch is resumed through its own thread list, and the answer is
// the same one the system-wide snapshot gives: one thread, resumed once.
func TestSoleThreadOfASuspendedLaunch(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	th, found, err := soleThreadOf(uint32(cmd.Process.Pid))
	if err != nil || !found {
		_ = cmd.Process.Kill()
		t.Fatalf("soleThreadOf = found %v, %v; want the per-process walk to answer", found, err)
	}
	if err := resumeSuspendedThread(th); err != nil {
		t.Fatalf("resume: %v", err)
	}
	_ = windows.CloseHandle(th)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the resumed process did not run to completion: %v", err)
	}
}
