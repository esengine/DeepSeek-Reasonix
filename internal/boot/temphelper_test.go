package boot

import (
	"os"
	"testing"
	"time"
)

// robustTempDir is a drop-in for t.TempDir whose cleanup retries RemoveAll for a
// short window. Tests here build a full Controller (Build / control.New); at
// teardown a background resource — a job goroutine draining after its context is
// cancelled, or an MCP stats/schema writer flushing — can still hold a file
// under the dir for a few milliseconds after Close returns. On Windows that
// surfaces as "being used by another process"; on Linux a write racing RemoveAll
// surfaces as "directory not empty". Plain t.TempDir turns that teardown race
// into a red test even though every assertion passed (this is the recurring
// main-v2 CI flake that #3371 only papered over). Retrying absorbs the race; a
// dir that never frees is logged, not fatal, so a genuine leak stays visible
// without reintroducing the flake.
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
