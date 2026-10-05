//go:build !windows

package cli

// hideFileWindows is a no-op on non-Windows platforms.
func hideFileWindows(_ string) {}
