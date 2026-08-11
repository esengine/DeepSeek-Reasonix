package boot

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/history"
	"reasonix/internal/historycatalog"
)

// isolateConfigHome redirects user config and cache paths to a per-test temp
// directory. The history projection is process-global, so it must be closed on
// both sides of the environment change instead of retaining a deleted cache
// path across repeated tests.
func isolateConfigHome(t *testing.T) string {
	t.Helper()
	closeBootTestHistoryCatalog(t)
	dir := robustTempDir(t)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv("LocalAppData", filepath.Join(dir, "LocalAppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Cleanup(func() { closeBootTestHistoryCatalog(t) })
	return dir
}

func closeBootTestHistoryCatalog(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := history.CloseSharedCatalog(ctx); err != nil {
		t.Fatalf("close shared history catalog: %v", err)
	}
}

// fenceBootTestHistoryCatalog releases a process-global projection inherited
// from an earlier test and closes the replacement before t.TempDir cleanup.
// Windows cannot remove a temporary REASONIX_HOME while SQLite still owns it.
func fenceBootTestHistoryCatalog(t *testing.T) {
	t.Helper()
	closeBootTestHistoryCatalog(t)
	t.Cleanup(func() { closeBootTestHistoryCatalog(t) })
}

func bootTestHistoryIndexReady(t *testing.T) <-chan struct{} {
	t.Helper()
	ready := make(chan struct{})
	var once sync.Once
	history.RegisterCatalogObserver(func(status historycatalog.Status, _ []string, _ string) {
		if status.Indexed > 0 {
			once.Do(func() { close(ready) })
		}
	})
	return ready
}

func waitForBootTestHistoryIndex(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for history catalog to index the saved fixture")
	}
}
