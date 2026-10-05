// edit_shell.go — the interpreter the shell tool hands commands to.
package config

import (
	"fmt"
	"strings"
)

// SetShell records which interpreter the shell tool runs commands under. An
// empty path keeps auto-detection of the executable, which is what every host
// without a custom install wants; pinning one survives a PATH that changes.
func (c *Config) SetShell(prefer, path string) error {
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "", "auto":
		c.Tools.Shell.Prefer = "auto"
	case "bash":
		c.Tools.Shell.Prefer = "bash"
	case "powershell":
		c.Tools.Shell.Prefer = "powershell"
	case "pwsh":
		c.Tools.Shell.Prefer = "pwsh"
	default:
		return fmt.Errorf("shell prefer %q: must be auto|bash|powershell|pwsh", prefer)
	}
	c.Tools.Shell.Path = strings.TrimSpace(path)
	return nil
}
