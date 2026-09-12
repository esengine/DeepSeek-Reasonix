package main

import "fmt"

// The permission posture is a benchmark arm shared by suite and SWE-bench
// mode: auto is the unattended default, and the alternative exists because
// comparable harnesses run without the dynamic-shell gate, so measuring
// against them under the gate measures our permission policy rather than the
// agent.
const (
	benchmarkPermissionAuto = "auto"
	benchmarkPermissionYolo = "yolo"
)

// permissionFlag maps a posture onto the CLI flag, rejecting anything else so
// an unknown value never runs unattended under a posture nobody chose.
func permissionFlag(mode string) (string, error) {
	switch mode {
	case "", benchmarkPermissionAuto:
		return "--permission-mode=auto", nil
	case benchmarkPermissionYolo:
		return "--permission-mode=bypassPermissions", nil
	default:
		return "", fmt.Errorf("unknown permission mode %q (want auto or yolo)", mode)
	}
}

// suitePermissionArg is permissionFlag for suite mode, where the historical
// spelling of the unattended default is the short --auto alias. An unknown
// value cannot reach here: main validates it before building the config.
func suitePermissionArg(mode string) string {
	if flagArg, err := permissionFlag(mode); err == nil && flagArg != "--permission-mode=auto" {
		return flagArg
	}
	return "--auto"
}
