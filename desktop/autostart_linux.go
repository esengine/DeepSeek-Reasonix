//go:build linux

package main

// autostartSupported reports whether autostart is supported on this platform.
// TODO: Implement using .desktop file in ~/.config/autostart/
func autostartSupported() bool { return false }

// isAutostartEnabled checks if autostart is enabled.
func isAutostartEnabled() bool { return false }

// setAutostart enables or disables autostart.
func setAutostart(enabled bool) error {
	// TODO: Implement using .desktop file in ~/.config/autostart/
	return nil
}
