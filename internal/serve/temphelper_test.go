package serve

import (
	"os"
	"testing"
	"time"
)

// robustTempDir tolerates Windows holding session-store handles (ownership
// locks, query-cache writes) briefly past test end: retry removal instead of
// failing the test on cleanup-only races.
func robustTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "reasonix-serve-test-*")
	if err != nil {
		t.Fatalf("robustTempDir: %v", err)
	}
	t.Cleanup(func() {
		var rmErr error
		for range 100 {
			if rmErr = os.RemoveAll(dir); rmErr == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Logf("robustTempDir: cleanup did not converge for %s: %v", dir, rmErr)
	})
	return dir
}
