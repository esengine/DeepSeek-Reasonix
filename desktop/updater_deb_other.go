//go:build !linux

package main

import "fmt"

var (
	errUpdateAuthFailed    = fmt.Errorf("update: authorization failed")
	errUpdateCacheMismatch = fmt.Errorf("update: cached artifact does not match current install mode")
)

func applyDebLinux(packagePath, signaturePath string, onPhase func(phase string)) error {
	return fmt.Errorf("update: deb install is only supported on Linux")
}

func isAuthCancelled(err error) bool {
	return false
}

func ensureDebCacheMatchesProfile(meta *cachedUpdate, profile installProfile) error {
	if profile.Mode == installModeDeb {
		return fmt.Errorf("update: deb install is only supported on Linux")
	}
	return nil
}
