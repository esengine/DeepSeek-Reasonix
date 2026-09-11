package usagecatalog

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"reasonix/internal/projectiondb"
)

// Rebuild replaces only the disposable usage projection after replaying the
// authoritative daily JSONL directory into a validated sibling database.
func Rebuild(ctx context.Context, path, statsDir string) (Status, error) {
	if path == "" {
		path = DefaultPath()
	}
	if strings.TrimSpace(path) == "" {
		catalog, err := Open(ctx, "")
		if err != nil {
			return Status{}, err
		}
		if err := catalog.ReconcileDir(ctx, statsDir); err != nil {
			closeRebuildCatalog(catalog)
			return catalog.Status(), err
		}
		status := catalog.Status()
		closeRebuildCatalog(catalog)
		return status, nil
	}
	err := projectiondb.Rebuild(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "usage-catalog-rebuild", Migrations: migrations(),
	}, func(ctx context.Context, db *sql.DB) error {
		catalog := &Catalog{db: db, status: Status{State: "ready", Mode: projectiondb.ModeDisk, Path: path}}
		return catalog.ReconcileDir(ctx, statsDir)
	})
	if err != nil {
		return Status{}, err
	}
	published, err := Open(ctx, path)
	if err != nil {
		return Status{}, err
	}
	status := published.Status()
	closeRebuildCatalog(published)
	return status, nil
}

func closeRebuildCatalog(catalog *Catalog) {
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = catalog.Close(closeCtx)
}
