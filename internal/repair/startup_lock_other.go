//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package repair

// lockRepairStateFile is a no-op on platforms without file locking; callers
// then behave as before the cross-process serialization was added.
func lockRepairStateFile(string) (func(), error) {
	return func() {}, nil
}
