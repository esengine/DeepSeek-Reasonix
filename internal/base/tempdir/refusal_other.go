//go:build !windows

package tempdir

// teardownRefusal is false everywhere else: one cleanup, one answer. A removal
// that failed failed, and waiting on it would only delay the report.
func teardownRefusal(error) bool { return false }
