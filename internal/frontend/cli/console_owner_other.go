//go:build !windows

package cli

func ownsConsoleAlone() bool { return false }
