//go:build !linux

package main

import "fmt"

var (
	errUpdateAuthCancelled = fmt.Errorf("update: authorization cancelled")
	errUpdateAuthFailed    = fmt.Errorf("update: authorization failed")
	errUpdatePkgBusy       = fmt.Errorf("update: package manager busy")
	errUpdatePkgInstall    = fmt.Errorf("update: package install failed")
	errUpdatePkgVerify     = fmt.Errorf("update: package verify failed")
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
