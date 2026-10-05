//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package mcplaunch

// Unsupported platforms fail closed for syntactically valid owners. A stale
// lock may require manual cleanup there, but a waiter must never steal a live
// writer's lock when the OS offers no portable process-liveness probe.
func launchLockProcessAlive(pid int) bool { return pid > 0 }
