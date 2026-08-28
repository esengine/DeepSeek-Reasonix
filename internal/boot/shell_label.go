package boot

import (
	"strings"

	"reasonix/internal/sandbox"
)

// resolvedShellLabel preserves the cache-stable kind label when the user did
// not configure an explicit path. When a path was configured, it reports the
// interpreter actually bound after validation and fallback, so a stale path
// can never describe a different executable.
func resolvedShellLabel(shell sandbox.Shell, configuredPath string) string {
	if strings.TrimSpace(configuredPath) != "" {
		if path := strings.TrimSpace(shell.Path); path != "" {
			return path
		}
	}
	return shell.Kind.String()
}
