//go:build windows

package sandbox

import "fmt"

func InspectCapabilityDevice(string) (CapabilityDevice, error) {
	return CapabilityDevice{}, fmt.Errorf("device identities are unsupported on Windows")
}
