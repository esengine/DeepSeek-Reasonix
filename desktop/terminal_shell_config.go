package main

import (
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/sandbox"
)

// terminalCommandFromConfig follows the shell tool's configured-path safety
// contract while keeping the integrated terminal's default-shell selection in
// its own owner. Rejected stale paths fall through to normal shell discovery.
func terminalCommandFromConfig(prefer, configuredPath string) (terminalCommand, bool) {
	prefer = strings.ToLower(strings.TrimSpace(prefer))
	if prefer == "" || prefer == "auto" {
		return terminalCommand{}, false
	}
	if prefer != "bash" && prefer != "powershell" && prefer != "pwsh" {
		return terminalCommand{}, false
	}
	configuredPath = sandbox.ConfiguredShellPathForPreference(prefer, configuredPath)
	if configuredPath != "" {
		if path, err := exec.LookPath(configuredPath); err == nil {
			label := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			return commandForShellPath(path, label), true
		}
	}
	resolved := sandbox.ResolveShell(prefer, "", nil)
	path, err := exec.LookPath(resolved.Path)
	if err != nil {
		return terminalCommand{}, false
	}
	label := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return commandForShellPath(path, label), true
}
