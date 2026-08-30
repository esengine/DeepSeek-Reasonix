//go:build darwin

package sandbox

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"
)

// gitCandidatePreflight avoids executing Apple's /usr/bin/git shim before the
// Command Line Tools are active. Invoking that shim on a fresh macOS install
// opens the system download prompt, which capability discovery must not do.
func gitCandidatePreflight(path string) bool {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	}
	if clean != "/usr/bin/git" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/xcode-select", "--print-path").Run() == nil
}
