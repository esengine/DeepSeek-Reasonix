package historycatalog

import (
	"context"
	"database/sql"

	"reasonix/internal/state/projectiondb"
)

// reportedFailures caps the sources a diagnostic names. The count covers every
// failed source; the names are the first ones to act on.
const reportedFailures = 5

// SupportedSchema is the newest projection schema this build migrates to. A
// catalog recording a higher one is what the runtime opens as degraded.
func SupportedSchema() int { return projectiondb.SupportedVersion(migrations()) }

// statusCounts are the numbers Status publishes.
type statusCounts struct {
	Indexed int64
	Total   int64
	Pending int64
	Failed  int64
}

// readStatusCounts is the one read behind both Status and Inspect, so a
// diagnostic and a reindex cannot disagree about what the catalog recorded.
func readStatusCounts(ctx context.Context, db *sql.DB) statusCounts {
	var counts statusCounts
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_sources WHERE health='ok'`).Scan(&counts.Indexed)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM history_roots`).Scan(&counts.Total)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_roots WHERE state<>'ready'`).Scan(&counts.Pending)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_sources WHERE health='corrupt'`).Scan(&counts.Failed)
	return counts
}

// Inspect reads what an existing history catalog recorded about the sources it
// could not index, without opening it for writing. Failed is the number
// `catalogs reindex --json` reports: both come from readStatusCounts.
func Inspect(ctx context.Context, path string) (projectiondb.Recorded, error) {
	db, err := projectiondb.OpenReadOnly(ctx, path)
	if err != nil {
		return projectiondb.Recorded{}, err
	}
	defer db.Close()
	counts := readStatusCounts(ctx, db)
	recorded := projectiondb.Recorded{Failed: counts.Failed, Pending: counts.Pending}
	rows, err := db.QueryContext(ctx, `SELECT path,health,last_error FROM history_sources
        WHERE health='corrupt' ORDER BY path LIMIT ?`, reportedFailures)
	if err != nil {
		return recorded, err
	}
	defer rows.Close()
	for rows.Next() {
		var failure projectiondb.Failure
		if err := rows.Scan(&failure.Path, &failure.Health, &failure.LastError); err != nil {
			return recorded, err
		}
		recorded.Failures = append(recorded.Failures, failure)
	}
	return recorded, rows.Err()
}
