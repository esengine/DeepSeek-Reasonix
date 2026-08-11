//go:build !darwin && !linux && !windows

package projectiondb

func filesystemRemote(string) bool { return false }
