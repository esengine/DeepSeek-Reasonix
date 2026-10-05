//go:build !windows

package termrender

func ANSIConsoleReady() bool { return true }
