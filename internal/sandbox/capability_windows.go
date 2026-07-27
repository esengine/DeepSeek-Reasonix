//go:build windows

package sandbox

import (
	"context"
	"fmt"
)

func capabilityPlatformNoDelta() bool { return true }

func capabilityBaseWriteRoots(spec Spec) []string {
	return append([]string(nil), spec.AppContainerWriteRoots...)
}

func capabilityBaseReadCovers(_ Spec, _ CapabilityPath) bool { return true }

func capabilityPlatformSupports(_ context.Context, _ Spec, _ CapabilitySet) (bool, string) {
	return false, "Windows Bash sandbox capabilities have no effective OS delta"
}

func prepareCapabilityPlatformLaunch(_ context.Context, _ Spec, _ CapabilitySet, _ Shell, _ string, _ []string) (CapabilityLaunch, error) {
	return CapabilityLaunch{}, fmt.Errorf("Windows Bash sandbox capabilities have no effective OS delta")
}
