//go:build !windows

package builtin

// isWindowsSharingViolation is a no-op outside Windows: sharing violations are
// a Windows-specific error class. The Office lock-file hint still applies via
// officeLockFile inside annotateWriteLockError.
func isWindowsSharingViolation(err error) bool { return false }
