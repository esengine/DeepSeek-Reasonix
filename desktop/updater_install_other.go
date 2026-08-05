//go:build !linux

package main

// detectLinuxInstallProfile is a stub on non-Linux platforms; detectInstallProfile
// never calls it outside the linux build tag, but the shared file references the
// name in comments only. Keep a no-op for documentation parity.
func detectLinuxInstallProfile() installProfile {
	return installProfile{
		Mode:          installModeManual,
		CanSelfUpdate: false,
		ManualReason:  "Linux install detection is unavailable on this platform",
	}
}
