//go:build !windows && !darwin

package main

func windowIsFrontmost() bool {
	// No reliable compositor focus probe on this build. Prefer summon over
	// hide so a hotkey press while another app is focused still raises Reasonix
	// instead of accidentally hiding a visible-but-unfocused window.
	return false
}
