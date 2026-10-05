//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package crashreport

// Without a liveness probe a dump is never read while its writer may still run.
func processAlive(pid int) bool { return pid > 0 }
