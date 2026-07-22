//go:build !linux

package sandbox

import (
	"fmt"
	"os"
)

// Non-Linux Adapters currently never enable path relaxation. Keep a defensive
// implementation for compile-time completeness; it is not an enforcement claim.
func openCapabilityDescriptor(path CapabilityPath) (*os.File, error) {
	file, err := os.Open(path.Canonical)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	kind, err := capabilityPathKind(info)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("path %q: %w", path.Canonical, err)
	}
	if kind != path.Kind {
		_ = file.Close()
		return nil, fmt.Errorf("path %q changed kind from %s to %s", path.Canonical, path.Kind, kind)
	}
	return file, nil
}
