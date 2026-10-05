package history

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/historycatalog"
)

// The process catalog outlives any one controller, so its close belongs at the
// process boundary. It went unclosed once already: the only caller lived in the
// retired desktop shell, and every launch after that re-scanned every root.
func TestCloseSharedCatalogReleasesTheProcessProjection(t *testing.T) {
	prevOpen := processHistoryCatalog.open
	t.Cleanup(func() {
		processHistoryCatalog = indexedCatalogManager{open: prevOpen}
	})

	dir := testenv.TempDir(t)
	processHistoryCatalog.register([]historycatalog.Root{{Path: dir, Source: "global", Scope: "global"}})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := FlushSharedCatalog(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := CloseSharedCatalog(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := processHistoryCatalog.get(); got != nil {
		t.Fatal("catalog still published after close")
	}
	// A second close is what a host that closes on both paths performs.
	if err := CloseSharedCatalog(ctx); err != nil {
		t.Fatalf("second close: %v", err)
	}
}
