//go:build windows

package sandbox

import (
	"context"
	"testing"
)

func TestWindowsDeviceCapabilityBackendIsUnsupported(t *testing.T) {
	ok, _ := capabilityPlatformSupports(context.Background(), Spec{Mode: "enforce"}, CapabilitySet{
		Devices: []CapabilityDevice{{Canonical: `\\.\PhysicalDrive0`, Kind: CapabilityBlockDevice}},
	})
	if ok || !capabilityPlatformNoDelta() {
		t.Fatal("Windows device capability backend must not claim enforcement")
	}
}
