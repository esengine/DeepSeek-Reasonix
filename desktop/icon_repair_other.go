//go:build !darwin && !windows

package main

// Linux package installs refresh the system icon and desktop caches from the
// package post-install script. Portable archives do not own desktop launchers.
func repairDesktopIconIntegration() error { return nil }
