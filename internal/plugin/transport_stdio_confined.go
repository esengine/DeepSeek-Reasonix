package plugin

import (
	"fmt"

	"reasonix/internal/sandbox"
)

// wrapConfinedCommand applies the OS command sandbox for a confined MCP
// process and fails closed when the backend is unavailable: a confined
// process must never run unsandboxed. commandArgs is injectable for tests.
func wrapConfinedCommand(name string, spec sandbox.Spec, launchArgs []string, commandArgs func(sandbox.Spec, []string) ([]string, bool)) ([]string, error) {
	argv, wrapped := commandArgs(spec, launchArgs)
	if spec.Enforce() && !wrapped {
		return nil, fmt.Errorf("stdio plugin %q: confined process requires an available sandbox backend", name)
	}
	return argv, nil
}
