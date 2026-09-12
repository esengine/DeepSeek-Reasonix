package installlayout

import (
	"os"
	"time"
)

const transientRetryAttempts = 10

// transientRetryDelay is a test seam; production backs off linearly to
// about eleven seconds, well inside the activation lock timeout.
var transientRetryDelay = time.Sleep

// retryTransient repeats op while it fails with a Windows sharing, lock, or
// access-denied error. Antivirus and indexers hold freshly written files for
// moments, and MoveFileEx/DeleteFile report that instead of waiting.
func retryTransient(op func() error) error {
	var err error
	for attempt := 1; attempt <= transientRetryAttempts; attempt++ {
		err = op()
		if err == nil || !transientFileError(err) || attempt == transientRetryAttempts {
			return err
		}
		transientRetryDelay(time.Duration(attempt) * 250 * time.Millisecond)
	}
	return err
}

func renameRetry(oldPath, newPath string) error {
	return retryTransient(func() error { return os.Rename(oldPath, newPath) })
}

func removeRetry(path string) error {
	return retryTransient(func() error { return os.Remove(path) })
}

func removeAllRetry(path string) error {
	return retryTransient(func() error { return os.RemoveAll(path) })
}
