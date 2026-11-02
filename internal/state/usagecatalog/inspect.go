package usagecatalog

import (
	"context"
	"database/sql"

	"reasonix/internal/state/projectiondb"
)

// SupportedSchema is the newest projection schema this build migrates to. A
// catalog recording a higher one is what the runtime opens as degraded.
func SupportedSchema() int { return projectiondb.SupportedVersion(migrations()) }

// readStatusCounts is the one read behind both Status and Inspect, so a
// diagnostic and a reindex cannot disagree about what the catalog recorded.
func readStatusCounts(ctx context.Context, db *sql.DB) (files, lag, corrupt int64) {
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(MAX(size-indexed_offset,0)),0),COALESCE(SUM(corrupt_lines),0) FROM usage_files`).Scan(&files, &lag, &corrupt)
	return files, lag, corrupt
}

// Inspect reads what an existing usage catalog recorded about the lines it
// could not read and the bytes it has not caught up to, without opening it for
// writing. Both numbers come from readStatusCounts, the read Status publishes.
func Inspect(ctx context.Context, path string) (projectiondb.Recorded, error) {
	db, err := projectiondb.OpenReadOnly(ctx, path)
	if err != nil {
		return projectiondb.Recorded{}, err
	}
	defer db.Close()
	_, lag, corrupt := readStatusCounts(ctx, db)
	return projectiondb.Recorded{LagBytes: lag, CorruptLines: corrupt}, nil
}
