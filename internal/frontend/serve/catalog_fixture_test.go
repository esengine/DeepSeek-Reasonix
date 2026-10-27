package serve

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/history"
)

func waitFixtureCatalog(t *testing.T, searcher *history.IndexedSearcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for searcher.IndexStatus().Path == "" {
		select {
		case <-ctx.Done():
			t.Fatal("fixture catalog did not open")
		case <-tick.C:
		}
	}
}

func TestTemporaryServeHomesRetireTheirSharedCatalog(t *testing.T) {
	for _, fixture := range []string{"unread", "provider_editor"} {
		t.Run(fixture, func(t *testing.T) {
			if err := history.CloseSharedCatalog(t.Context()); err != nil {
				t.Fatal(err)
			}
			closeSharedCatalogsOnCleanup(t)
			var searcher *history.IndexedSearcher
			t.Run("temporary_home", func(t *testing.T) {
				t.Setenv("REASONIX_CACHE_HOME", filepath.Join(testenv.TempDir(t), "cache"))
				root := ""
				if fixture == "unread" {
					root = newUnreadHarness(t, "plain").root
				} else {
					_ = newProviderEditServer(t)
					root = testenv.TempDir(t)
				}
				searcher = history.NewIndexedSearcher(history.Options{SessionDir: root})
				waitFixtureCatalog(t, searcher)
			})
			if searcher != nil && searcher.IndexStatus().Path != "" {
				t.Error("shared catalog retained a temporary home's database after fixture cleanup")
			}
		})
	}
}
