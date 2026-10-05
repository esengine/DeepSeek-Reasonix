package cli

import (
	"context"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/history"
	"reasonix/internal/state/stats"
)

// Process-level projections, closed at the process boundary rather than with a
// controller: one process can own several controllers (desktop tabs, serve
// workspaces) and they share these catalogs, so a per-controller close would
// pull the projection out from under the others.
func closeCLIUsageCatalogs() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = stats.Flush(ctx, config.StatsDir())
	_ = stats.CloseUsageCatalogs(ctx)
	// Drain queued index writes before cancelling them; the reopen that follows
	// an unclean exit re-scans every root instead.
	_ = history.FlushSharedCatalog(ctx)
	_ = history.CloseSharedCatalog(ctx)
}
