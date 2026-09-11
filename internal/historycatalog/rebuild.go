package historycatalog

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"reasonix/internal/projectiondb"
)

// Rebuild replaces only the disposable FTS projection after indexing every
// supplied authoritative root into a validated sibling database.
func Rebuild(ctx context.Context, opts Options, roots []Root) (Status, error) {
	opts = rebuildOptions(opts)
	if opts.InMemory || strings.TrimSpace(opts.Path) == "" {
		catalog, err := Open(ctx, opts)
		if err != nil {
			return Status{}, err
		}
		for _, root := range roots {
			if err := catalog.ReconcileRoot(ctx, root); err != nil {
				closeRebuildCatalog(catalog)
				return catalog.Status(), err
			}
		}
		status := catalog.Status()
		closeRebuildCatalog(catalog)
		return status, nil
	}
	err := projectiondb.Rebuild(ctx, projectiondb.OpenOptions{
		Path: opts.Path, MemoryName: "history-search-rebuild", Migrations: migrations(),
		SecureDelete: true, AutoVacuum: true,
	}, func(ctx context.Context, db *sql.DB) error {
		catalog := &Catalog{db: db, opts: opts, status: Status{State: "ready", Mode: projectiondb.ModeDisk, Path: opts.Path}}
		for _, root := range roots {
			if err := catalog.ReconcileRoot(ctx, root); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	published, err := Open(ctx, opts)
	if err != nil {
		return Status{}, err
	}
	status := published.Status()
	closeRebuildCatalog(published)
	return status, nil
}

func rebuildOptions(opts Options) Options {
	if opts.Path == "" {
		opts.Path = DefaultPath()
	}
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path, opts.InMemory = "", true
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MissingGrace <= 0 {
		opts.MissingGrace = defaultMissingGrace
	}
	opts.OnRevision = nil
	return opts
}

func closeRebuildCatalog(catalog *Catalog) {
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = catalog.Close(closeCtx)
}
