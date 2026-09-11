//go:build !windows

package builtin

// isWindowsFileLocked is a no-op outside Windows: sharing and access-denied
// errnos are a Windows-specific error class. The Office lock-file hint still
// applies via officeLockFile inside annotateWriteLockError.
func isWindowsFileLocked(err error) bool { return false }
