package main

import (
	"os"
	"testing"
	"time"
)

// robustTempDir is a drop-in for t.TempDir whose cleanup retries RemoveAll for a
// short window. The Capabilities/history tests wire a Controller and isolate the
// user-config tree under a temp dir; at teardown a background resource can still
// hold a file under it for a few milliseconds after Close returns. On Windows
// that surfaces as "being used by another process"; on Linux a write racing
// RemoveAll surfaces as "directory not empty". Plain t.TempDir turns that
// teardown race into a red test even though every assertion passed (the
// recurring main-v2 CI flake). Retrying absorbs the race; a dir that never frees
// is logged, not fatal, so a genuine leak stays visible without the flake.
func robustTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "reasonix-test-*")
	if err != nil {
		t.Fatalf("robustTempDir: %v", err)
	}
	t.Cleanup(func() {
		var rmErr error
		for i := 0; i < 100; i++ {
			if rmErr = os.RemoveAll(dir); rmErr == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("robustTempDir: cleanup did not converge for %s: %v", dir, rmErr)
	})
	return dir
}
