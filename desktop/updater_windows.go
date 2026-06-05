//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"syscall"
)

// installerCommand runs the NSIS updater, forcing $INSTDIR to dir via /D= so the
// update overwrites the current install in place. NSIS requires /D= to be the
// final, unquoted token taken verbatim to the end of the line, so the raw command
// line is set directly — exec.Command would quote a path containing spaces (e.g.
// C:\Users\Jane Doe\...) and NSIS would then mis-parse the target directory.
func installerCommand(name, dir string) *exec.Cmd {
	cmd := exec.Command(name)
	if dir != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`"%s" /D=%s`, name, dir)}
	}
	return cmd
}
