//go:build !windows && !darwin

package main

func windowIsFrontmost() bool {
	// Without a reliable compositor focus probe, treat a non-backgrounded
	// window as frontmost so the hotkey still toggles hide/show.
	return true
}
