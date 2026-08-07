//go:build windows

package remote

import (
	"os/exec"
	"syscall"
)

func newProxyCommandProcess(command string) *exec.Cmd {
	cmd := exec.Command("cmd.exe")
	cmd.Args = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + command + `"`}
	return cmd
}
