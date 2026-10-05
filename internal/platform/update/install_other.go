//go:build !windows

package update

import (
	"os/exec"

	"reasonix/internal/platform/repair"
)

// installerCommand exists only so the handoff compiles off Windows; the Windows
// apply path is never dispatched there.
func installerCommand(name, _ string) *exec.Cmd {
	return exec.Command(name)
}

func (h WindowsHandoff) Start(_ *repair.UpdateTransaction) error {
	return installerCommand(h.InstallerPath, h.InstallDir).Start()
}

func (h WindowsHandoff) StartVersioned(_ string) error {
	return installerCommand(h.InstallerPath, h.InstallDir).Start()
}
