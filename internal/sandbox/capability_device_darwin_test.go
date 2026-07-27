//go:build darwin

package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestDarwinDeviceCapabilityBackendIsUnsupported(t *testing.T) {
	ok, reason := capabilityPlatformSupports(context.Background(), Spec{Mode: "enforce"}, CapabilitySet{
		Devices: []CapabilityDevice{{Canonical: "/dev/null", Kind: CapabilityCharacterDevice}},
	})
	if ok || !strings.Contains(reason, "Linux") {
		t.Fatalf("support = %v, reason=%q; want explicit unsupported device backend", ok, reason)
	}
}
