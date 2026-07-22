//go:build windows

package sandbox

import "fmt"

func inspectCapabilityDevice(string) (CapabilityDevice, error) {
	return CapabilityDevice{}, fmt.Errorf("device identities are unsupported on Windows")
}
