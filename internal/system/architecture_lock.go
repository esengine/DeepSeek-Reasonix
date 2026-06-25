package system

import (
	"errors"
	"fmt"
	"strings"
)

const ArchitectureLocked = true

var (
	errContractMismatch = errors.New("system contract does not match frozen v5 stable release boundary")
	errChecksIncomplete = errors.New("production readiness checks are incomplete")
)

var forbiddenArchitectureActions = map[string]struct{}{
	"control_plane_modification": {},
	"execution_model_change":     {},
	"equilibrium_rewrite":        {},
	"runtime_topology_change":    {},
	"v6_field_model_activation":  {},
}

// EnforceArchitectureLock ensures no runtime system modification beyond the v5
// stable release scope can pass through the freeze boundary.
func EnforceArchitectureLock(component string, action string) error {
	if !ArchitectureLocked {
		return nil
	}
	component = strings.TrimSpace(component)
	action = strings.TrimSpace(action)
	if component == "" {
		component = "unknown"
	}
	if _, forbidden := forbiddenArchitectureActions[action]; forbidden {
		return fmt.Errorf("ARCHITECTURE_LOCKED: %s not allowed on %s", action, component)
	}
	return nil
}
