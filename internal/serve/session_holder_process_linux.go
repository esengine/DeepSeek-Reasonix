//go:build linux

package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func platformReasonixSessionHolderProcess(pid int) bool {
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(executable)), " (deleted)")
	return name == "reasonix" || name == "reasonix-cli"
}

func platformTerminateReasonixSessionHolder(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
