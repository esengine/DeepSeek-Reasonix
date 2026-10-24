package session

import (
	"context"

	"reasonix/internal/projectiondb"
)

// Session read indexes are disposable projections. Every read entry point
// shares the same verified-file policy; authoritative stores keep full audits.
func openHistoryProjection(ctx context.Context, path string) (*projectiondb.Handle, error) {
	return projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations,
		RequireDisk: true, MaxOpenConns: 1, ReuseIntegrityCheck: true})
}

func openSearchProjection(ctx context.Context, path string) (*projectiondb.Handle, error) {
	return projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: searchMigrations,
		RequireDisk: true, MaxOpenConns: 1, ReuseIntegrityCheck: true})
}
