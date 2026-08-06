//go:build !windows

package cli

func ansiConsoleReady() bool { return true }
