package sessioncatalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/agent"
)

const (
	repairRetryInitial = 30 * time.Second
	repairRetryMaximum = 30 * time.Minute
	repairRetryReset   = 2 * repairRetryMaximum
)

type repairRetryState struct {
	failures          int
	failedAt          time.Time
	retryAt           time.Time
	sourceFingerprint string
}

func (c *Catalog) enqueueRepair(path string) bool {
	if c == nil || c.opts.DisableRepair || strings.TrimSpace(path) == "" {
		return false
	}
	key := c.pathKey(path)
	if !c.repairReady(path) {
		return false
	}
	if _, loaded := c.repairQueued.LoadOrStore(key, struct{}{}); loaded {
		return false
	}
	select {
	case c.repairCh <- path:
		return true
	case <-c.stop:
		c.repairQueued.Delete(key)
		return false
	default:
		// Channel pressure must never permanently drop unknown rows. Leave them
		// in the DB and clear the in-memory marker so the drain ticker requeues.
		c.repairQueued.Delete(key)
		return false
	}
}

func (c *Catalog) repairNow() time.Time {
	if c != nil && c.opts.Now != nil {
		return c.opts.Now()
	}
	return time.Now()
}

func (c *Catalog) repairReadyKey(key string) bool {
	value, ok := c.repairRetry.Load(key)
	if !ok {
		return true
	}
	state := value.(repairRetryState)
	return !c.repairNow().Before(state.retryAt)
}

func (c *Catalog) repairReady(path string) bool {
	key := c.pathKey(path)
	for {
		value, ok := c.repairRetry.Load(key)
		if !ok {
			return true
		}
		state := value.(repairRetryState)
		if fileFingerprint(path) == state.sourceFingerprint {
			return !c.repairNow().Before(state.retryAt)
		}
		if c.repairRetry.CompareAndDelete(key, value) {
			return true
		}
	}
}

func (c *Catalog) recordRepairFailure(path string) {
	key := c.pathKey(path)
	now := c.repairNow()
	failures := 1
	if value, ok := c.repairRetry.Load(key); ok {
		state := value.(repairRetryState)
		elapsed := now.Sub(state.failedAt)
		if !state.failedAt.IsZero() && elapsed >= 0 && elapsed <= repairRetryReset {
			failures = state.failures + 1
		}
	}
	delay := repairRetryInitial
	for attempt := 1; attempt < failures && delay < repairRetryMaximum; attempt++ {
		delay *= 2
		if delay > repairRetryMaximum {
			delay = repairRetryMaximum
		}
	}
	c.repairRetry.Store(key, repairRetryState{
		failures:          failures,
		failedAt:          now,
		retryAt:           now.Add(delay),
		sourceFingerprint: fileFingerprint(path),
	})
}

func (c *Catalog) clearRepairFailure(path string) {
	c.repairRetry.Delete(c.pathKey(path))
}

func (c *Catalog) repairScanLimit(limit int) int {
	scanLimit := limit
	c.repairRetry.Range(func(key, _ any) bool {
		if !c.repairReadyKey(key.(string)) {
			scanLimit++
		}
		return true
	})
	c.repairQueued.Range(func(_, _ any) bool {
		scanLimit++
		return true
	})
	return scanLimit
}

func (c *Catalog) enqueuePersistedRepairs(ctx context.Context) {
	c.drainUnknownRepairs(ctx, c.opts.QueueCapacity)
}

// drainUnknownRepairs pulls the next eligible turns_state=unknown paths from
// the durable projection. It scans past backed-off or already queued rows so
// they cannot consume the batch limit and starve later repairs.
func (c *Catalog) drainUnknownRepairs(ctx context.Context, limit int) {
	if c == nil || c.db == nil || c.opts.DisableRepair || limit <= 0 {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	queuedSignals := len(c.repairCh)
	if queuedSignals >= limit || (cap(c.repairCh) > 0 && queuedSignals >= cap(c.repairCh)) {
		return
	}
	scanLimit := c.repairScanLimit(limit)
	rows, err := c.db.QueryContext(ctx, `SELECT path FROM catalog_sessions
		WHERE turns_state='unknown' ORDER BY last_activity_at DESC,path_key ASC LIMIT ?`, scanLimit)
	if err != nil {
		return
	}
	defer rows.Close()
	queued := 0
	for rows.Next() {
		var path string
		if rows.Scan(&path) != nil {
			continue
		}
		if c.enqueueRepair(path) {
			queued++
		}
		if queued >= limit || (cap(c.repairCh) > 0 && len(c.repairCh) >= cap(c.repairCh)) {
			return
		}
	}
}

func (c *Catalog) repairLoop() {
	defer c.workers.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case path := <-c.repairCh:
			repaired := c.repairSession(c.workerCtx, path)
			if repaired {
				c.clearRepairFailure(path)
			} else if c.workerCtx.Err() == nil {
				c.recordRepairFailure(path)
			}
			c.repairQueued.Delete(c.pathKey(path))
			c.drainUnknownRepairs(c.workerCtx, 32)
			runtime.Gosched()
		case <-ticker.C:
			c.drainUnknownRepairs(c.workerCtx, 64)
		case <-c.stop:
			return
		}
	}
}

func (c *Catalog) repairSession(workerCtx context.Context, path string) bool {
	if workerCtx.Err() != nil {
		return false
	}
	// Repair writes a source snapshot that the next directory projection will
	// consume. Share the directory lock with exact indexing and reconcile so a
	// scan parsed before this repair cannot overwrite its result afterward.
	lock := c.directoryLock(filepath.Dir(path))
	if c.testRepairLockHook != nil {
		c.testRepairLockHook(false)
	}
	lock.Lock()
	defer lock.Unlock()
	if c.testRepairLockHook != nil {
		c.testRepairLockHook(true)
	}

	ctx, cancel := context.WithTimeout(workerCtx, 30*time.Second)
	defer cancel()
	// LoadSessionDisplayMessages is not yet context-aware; check before/after.
	msgs, state, _, err := agent.LoadSessionDisplayMessages(path)
	if ctx.Err() != nil || workerCtx.Err() != nil {
		return false
	}
	if err != nil {
		return c.applyRepairResult(ctx, path, "", 0, false) == nil
	}
	preview, turns := agent.SessionPreviewFromMessages(msgs)
	applied, err := agent.UpdateSessionListingProjectionIfCurrent(path, "", preview, turns, false, state)
	if err != nil || !applied {
		return false
	}
	if ctx.Err() != nil || workerCtx.Err() != nil {
		return false
	}
	return c.applyRepairResult(ctx, path, preview, turns, true) == nil
}

// applyRepairResult updates only fields proven by parsing one transcript. Topic
// aggregates and recovery projection fields remain owned by ReconcileDirectory.
// The caller holds the directory lock, so the queued reconcile observes this
// source state or a newer one and publishes exactly one complete projection.
func (c *Catalog) applyRepairResult(ctx context.Context, path, preview string, turns int, valid bool) error {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var target DirectoryTarget
	pathKey := c.pathKey(path)
	if err := tx.QueryRowContext(ctx, `SELECT directory,scope,workspace_root FROM catalog_sessions WHERE path_key=?`, pathKey).
		Scan(&target.Path, &target.Scope, &target.WorkspaceRoot); err != nil {
		_ = tx.Rollback()
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if valid {
		_, err = tx.ExecContext(ctx, `UPDATE catalog_sessions SET preview=?,turns=?,turns_state='valid',
			health='ok',meta_fingerprint=? WHERE path_key=?`,
			preview, turns, fileFingerprint(agent.BranchMetaPath(path)), pathKey)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE catalog_sessions SET turns_state='corrupt',health='corrupt' WHERE path_key=?`, pathKey)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.refreshCounts(ctx)
	c.RequestReconcile(target)
	return nil
}

type knownSourceState struct {
	preview            string
	turns              int
	turnsState         TurnsState
	health             Health
	contentFingerprint string
}

// preserveKnownSourceStates prevents a directory scan backed by a legacy or
// transient sidecar from replacing a repaired valid/corrupt source result with
// unknown. The content fingerprint guard makes a changed transcript unknown
// again until that new generation has been parsed.
func (c *Catalog) preserveKnownSourceStates(ctx context.Context, directory string, records []SessionRecord) ([]SessionRecord, error) {
	needsKnownState := false
	for i := range records {
		if records[i].TurnsState == TurnsUnknown {
			needsKnownState = true
			break
		}
	}
	if !needsKnownState {
		return records, nil
	}
	rows, err := c.db.QueryContext(ctx, `SELECT path,preview,turns,turns_state,health,content_fingerprint
		FROM catalog_sessions WHERE directory_key=? AND missing_since=0 AND turns_state<>'unknown'`, c.pathKey(directory))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known := make(map[string]knownSourceState)
	for rows.Next() {
		var path string
		var state knownSourceState
		if err := rows.Scan(&path, &state.preview, &state.turns, &state.turnsState, &state.health, &state.contentFingerprint); err != nil {
			return nil, err
		}
		known[c.pathKey(path)] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range records {
		state, ok := known[c.pathKey(records[i].Path)]
		if !ok || records[i].TurnsState != TurnsUnknown || records[i].ContentFingerprint != state.contentFingerprint {
			continue
		}
		records[i].Preview = state.preview
		records[i].Turns = state.turns
		records[i].TurnsState = state.turnsState
		records[i].Health = state.health
	}
	return records, nil
}
