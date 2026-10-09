package boot

import (
	"testing"

	"reasonix/internal/base/testenv"
)

// robustTempDir is testenv.TempDir, whose cleanup retries RemoveAll for
// handles that outlive Close by milliseconds. The process-global catalogs are
// closed first: an open SQLite file outlasts every retry on Windows.
func robustTempDir(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	t.Cleanup(func() { closeBootTestHistoryCatalog(t) })
	return dir
}
