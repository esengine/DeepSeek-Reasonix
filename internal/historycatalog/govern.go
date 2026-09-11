package historycatalog

import (
	"context"
	"database/sql"
	"os"
	"strings"

	"reasonix/internal/config"
)

const (
	// DefaultMaxBytes caps the disposable index; session files stay authoritative.
	DefaultMaxBytes = 256 << 20
	// rebuildOversizeFactor: past this multiple of the cap a background
	// wipe+rebuild beats evicting nearly every session, and applies tool-text
	// truncation to legacy rows (#8717).
	rebuildOversizeFactor = 2
	// evictTargetPercent: reclaim down to this share of the cap so the next
	// persist batch does not immediately re-trigger eviction.
	evictTargetPercent = 80
	maxEvictRounds     = 8
)

func resolveMaxBytes(option int64, configuredMB int) int64 {
	if option > 0 {
		return option
	}
	if configuredMB > 0 {
		return int64(configuredMB) << 20
	}
	return DefaultMaxBytes
}

func configuredMaxMB() int {
	return config.HistorySearchMaxMB()
}

func historyDBFileSize(path string) int64 {
	var total int64
	for _, candidate := range []string{path, path + "-wal"} {
		if info, err := os.Stat(candidate); err == nil {
			total += info.Size()
		}
	}
	return total
}

// governSize enforces the on-disk size cap. Best-effort: failures surface via
// status.LastError and the next reconcile tick retries.
func (c *Catalog) governSize(ctx context.Context) {
	if c.opts.MaxBytes <= 0 || c.opts.InMemory || strings.TrimSpace(c.opts.Path) == "" {
		return
	}
	size := historyDBFileSize(c.opts.Path)
	if size <= c.opts.MaxBytes {
		return
	}
	// Live WAL bytes are not reclaimable; fold them before measuring again.
	_, _ = c.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	size = historyDBFileSize(c.opts.Path)
	if size <= c.opts.MaxBytes {
		return
	}
	if size > rebuildOversizeFactor*c.opts.MaxBytes {
		c.wipeForRebuild(ctx)
		return
	}
	c.evictToTarget(ctx, size)
}

func wipeProjectionRows(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{`DELETE FROM history_fts`, `DELETE FROM history_documents`,
		`DELETE FROM history_sources`, `DELETE FROM history_roots`} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// reclaimDiskSpace collapses FTS delete tombstones and returns freed pages to
// the OS. auto_vacuum never stuck on these pooled handles, so
// incremental_vacuum is a no-op here; VACUUM is the only working reclaim, and
// in WAL mode its pages land in the WAL first, so the checkpoint follows it.
func (c *Catalog) reclaimDiskSpace(ctx context.Context) {
	_, _ = c.db.ExecContext(ctx, `INSERT INTO history_fts(history_fts) VALUES('optimize')`)
	_, _ = c.db.ExecContext(ctx, `VACUUM`)
	_, _ = c.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
}

// wipeForRebuild drops the whole projection so roots rescan under current
// indexing rules; source session files are never touched.
func (c *Catalog) wipeForRebuild(ctx context.Context) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		c.setError(err)
		return
	}
	if err := wipeProjectionRows(ctx, tx); err != nil {
		_ = tx.Rollback()
		c.setError(err)
		return
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		c.setError(err)
		return
	}
	if err := tx.Commit(); err != nil {
		c.setError(err)
		return
	}
	c.reclaimDiskSpace(ctx)
	c.markAllRootsDirty()
	c.publish(revision, nil, "rebuild-oversize")
}

func (c *Catalog) evictToTarget(ctx context.Context, size int64) {
	target := c.opts.MaxBytes * evictTargetPercent / 100
	for range maxEvictRounds {
		if size <= target {
			return
		}
		evicted, err := c.evictOldestBatch(ctx, size, size-target)
		if err != nil {
			c.setError(err)
			return
		}
		if evicted == 0 {
			return
		}
		c.reclaimDiskSpace(ctx)
		size = historyDBFileSize(c.opts.Path)
	}
}

// evictOldestBatch drops index rows for the least-recently-active sessions
// whose estimated footprint covers overage (always at least one). The
// history_sources row survives as health='evicted' so an unchanged file is not
// indexed back in on the next rescan; it fully re-indexes on its next content
// change. Source session files are never touched.
func (c *Catalog) evictOldestBatch(ctx context.Context, size, overage int64) (int, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT s.path,COALESCE(SUM(d.token_count),0)
        FROM history_sources s JOIN history_documents d ON d.source_path=s.path
        GROUP BY s.path ORDER BY s.last_activity_at ASC,s.path ASC`)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		path   string
		tokens int64
	}
	candidates := []candidate{}
	var totalTokens int64
	for rows.Next() {
		var cand candidate
		if err := rows.Scan(&cand.path, &cand.tokens); err != nil {
			_ = rows.Close()
			return 0, err
		}
		candidates = append(candidates, cand)
		totalTokens += cand.tokens
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	_ = rows.Close()
	if len(candidates) == 0 || totalTokens <= 0 {
		return 0, nil
	}
	// Self-calibrating estimate of file bytes per indexed token (fixed overhead
	// included), so the prefix errs towards evicting too few per round rather
	// than too many; the round loop re-measures and converges.
	bytesPerToken := float64(size) / float64(totalTokens)
	batch := []string{}
	covered := 0.0
	for _, cand := range candidates {
		batch = append(batch, cand.path)
		covered += float64(cand.tokens) * bytesPerToken
		if covered >= float64(overage) {
			break
		}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for _, path := range batch {
		for _, statement := range []string{
			`DELETE FROM history_fts WHERE rowid IN (SELECT id FROM history_documents WHERE source_path=?)`,
			`DELETE FROM history_documents WHERE source_path=?`,
		} {
			if _, err := tx.ExecContext(ctx, statement, path); err != nil {
				_ = tx.Rollback()
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE history_sources SET health='evicted' WHERE path=?`, path); err != nil {
			_ = tx.Rollback()
			return 0, err
		}
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	c.publish(revision, nil, "evict")
	return len(batch), nil
}
