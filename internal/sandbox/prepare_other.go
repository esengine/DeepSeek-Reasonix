//go:build !linux

package sandbox

func isLinux() bool { return false }
