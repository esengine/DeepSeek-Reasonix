//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package cli

func queryTerminalBackground() (terminalRGB, bool) {
	return terminalRGB{}, false
}
