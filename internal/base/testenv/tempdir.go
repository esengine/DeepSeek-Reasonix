package testenv

import (
	"os"
	"time"
)

// TB is the subset of *testing.T that TempDir needs, declared here so this
// package stays free of a testing import the way TestingM already does.
type TB interface {
	Helper()
	Cleanup(func())
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

const (
	tempDirRemoveAttempts = 100
	tempDirRemoveBackoff  = 20 * time.Millisecond
)

// TempDir is a drop-in for t.TempDir whose cleanup retries RemoveAll. A test
// can finish while the OS still holds a file it wrote: Windows keeps a deleted
// file's directory entry until its last handle closes, so removing the
// directory fails as non-empty when every file is already gone. Measured at ~2%
// of session saves, always inside 10ms; one that never frees is logged, not fatal.
func TempDir(t TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "reasonix-test-*")
	if err != nil {
		t.Fatalf("testenv.TempDir: %v", err)
		return ""
	}
	t.Cleanup(func() {
		var rmErr error
		for range tempDirRemoveAttempts {
			if rmErr = os.RemoveAll(dir); rmErr == nil {
				return
			}
			time.Sleep(tempDirRemoveBackoff)
		}
		t.Logf("testenv.TempDir: cleanup did not converge for %s: %v", dir, rmErr)
	})
	return dir
}
