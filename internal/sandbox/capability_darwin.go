//go:build darwin

package sandbox

import (
	"context"
	"fmt"
)

func capabilityPlatformNoDelta() bool { return false }

func capabilityBaseWriteRoots(spec Spec) []string {
	return writeAllowDirsForSpec(spec)
}

func capabilityBaseReadCovers(_ Spec, _ CapabilityPath) bool { return true }

func capabilityPlatformSupports(_ context.Context, _ Spec, delta CapabilitySet) (bool, string) {
	if len(delta.Devices) > 0 {
		return false, "device capabilities are supported only by the proven Linux Bubblewrap backend"
	}
	if len(delta.Reads)+len(delta.Writes) > 0 {
		return false, "Seatbelt path exceptions have not passed the required real-platform safety probe"
	}
	return true, ""
}

func prepareCapabilityPlatformLaunch(_ context.Context, base Spec, delta CapabilitySet, sh Shell, command string, directArgv []string) (CapabilityLaunch, error) {
	if len(delta.Devices) > 0 {
		return CapabilityLaunch{}, fmt.Errorf("device capabilities are unsupported on macOS")
	}
	if len(delta.Reads)+len(delta.Writes) > 0 {
		return CapabilityLaunch{}, fmt.Errorf("Seatbelt path exceptions are unsupported")
	}
	spec := cloneSpec(base)
	if delta.Network {
		spec.Network = true
	}
	argv, wrapped := Command(spec, sh, command)
	if len(directArgv) > 0 {
		argv, wrapped = CommandArgs(spec, directArgv)
	}
	if !wrapped {
		return CapabilityLaunch{}, fmt.Errorf("sandbox-exec is unavailable")
	}
	return CapabilityLaunch{Argv: argv, Wrapped: true}, nil
}
