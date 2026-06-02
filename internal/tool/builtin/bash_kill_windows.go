package builtin

import (
	"os/exec"
	"strconv"
	"syscall"
)

// The agent host can be a GUI process with no console of its own (the Wails
// desktop app), so a console child like cmd.exe/powershell pops its own window
// unless we ask for none. CREATE_NO_WINDOW isn't exported by syscall.
const createNoWindow = 0x08000000

// setKillTree hides the child's console and makes a cancelled command kill its
// whole process tree. Windows does not cascade a kill to child processes, so
// killing the shell leaves `go test` and the binaries it spawned running after an
// Esc; taskkill /T walks the PID tree and /F forces it.
func setKillTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
		_ = kill.Run()
		return cmd.Process.Kill()
	}
}
