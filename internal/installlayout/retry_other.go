//go:build !windows

package installlayout

func transientFileError(error) bool { return false }
