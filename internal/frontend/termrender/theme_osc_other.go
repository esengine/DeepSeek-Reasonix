//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package termrender

func queryTerminalBackground() (terminalRGB, bool) {
	return terminalRGB{}, false
}
