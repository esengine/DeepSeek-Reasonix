package main

import "fmt"

// benchmarkPermissionDefault is the posture every recorded run used before the
// flag reached suite mode: the same workspace writes the fixtures require.
const benchmarkPermissionDefault = "workspace-write"

// permissionFlag maps the benchmark preset onto the same public CLI contract
// used by every other entry point. Both suite and SWE-bench mode go through it,
// so an unknown value never runs unattended under a posture nobody chose.
func permissionFlag(mode string) (string, error) {
	switch mode {
	case "", benchmarkPermissionDefault:
		return "--permission-mode=workspace-write", nil
	case "read-only":
		return "--permission-mode=read-only", nil
	case "danger-full-access":
		return "--permission-mode=danger-full-access", nil
	default:
		return "", fmt.Errorf("unknown permission preset %q (want read-only, workspace-write, or danger-full-access)", mode)
	}
}

// mustPermissionFlag resolves the posture for an invocation the suite builds
// itself. Every caller runs after runSuiteMode validated the preset, so an
// error here is a wiring bug rather than operator input.
func mustPermissionFlag(mode string) string {
	arg, err := permissionFlag(mode)
	if err != nil {
		panic(err)
	}
	return arg
}
