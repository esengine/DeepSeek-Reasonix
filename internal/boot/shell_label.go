package boot

import (
	"strings"

	"reasonix/internal/sandbox"
)

// resolvedShellLabel keeps the provider-visible environment section aligned
// with the interpreter actually bound after configured-path validation and
// fallback. A stale persisted path must never describe a different executable.
func resolvedShellLabel(shell sandbox.Shell) string {
	if path := strings.TrimSpace(shell.Path); path != "" {
		return path
	}
	return shell.Kind.String()
}
