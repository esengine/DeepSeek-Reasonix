//go:build !windows

package remote

import "os/exec"

func newProxyCommandProcess(command string) *exec.Cmd {
	return exec.Command("sh", "-c", "exec "+command)
}
