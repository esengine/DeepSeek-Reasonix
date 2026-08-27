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

func (c *Catalog) enqueueRepair(path string) {
	if c == nil || c.opts.DisableRepair || strings.TrimSpace(path) == "" {
		return
	}
	if _, loaded := c.repairQueued.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	select {
	case c.repairCh <- path:
	case <-c.stop:
		c.repairQueued.Delete(path)
	default:
		// Channel pressure must never permanently drop unknown rows. Leave them
		// in the DB and clear the in-memory marker so the drain ticker requeues.
		c.repairQueued.Delete(path)
	}
}

func (c *Catalog) enqueuePersistedRepairs(ctx context.Context) {
	c.drainUnknownRepairs(ctx, c.opts.QueueCapacity)
}

// drainUnknownRepairs pulls the next batch of turns_state=unknown paths from the
// durable projection. Combined with the repair ticker this gives eventual
// completeness even when more than QueueCapacity sessions need repair.
func (c *Catalog) drainUnknownRepairs(ctx context.Context, limit int) {
	if c == nil || c.db == nil || c.opts.DisableRepair || limit <= 0 {
		return
	}
	if err := ctx.Err(); err != nil {
		return
	}
	rows, err := c.db.QueryContext(ctx, `SELECT path FROM catalog_sessions
        WHERE turns_state='unknown' ORDER BY last_activity_at DESC LIMIT ?`, limit)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if rows.Scan(&path) == nil {
			c.enqueueRepair(path)
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
			c.repairSession(c.workerCtx, path)
			c.repairQueued.Delete(path)
			c.drainUnknownRepairs(c.workerCtx, 32)
			runtime.Gosched()
		case <-ticker.C:
			c.drainUnknownRepairs(c.workerCtx, 64)
		case <-c.stop:
			return
		}
	}
}

func (c *Catalog) repairSession(workerCtx context.Context, path string) {
	if workerCtx.Err() != nil {
		return
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
		return
	}
	if err != nil {
		_ = c.applyRepairResult(ctx, path, "", 0, false)
		return
	}
	preview, turns := agent.SessionPreviewFromMessages(msgs)
	applied, err := agent.UpdateSessionListingProjectionIfCurrent(path, "", preview, turns, false, state)
	if err != nil || !applied {
		return
	}
	if ctx.Err() != nil || workerCtx.Err() != nil {
		return
	}
	_ = c.applyRepairResult(ctx, path, preview, turns, true)
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
	if err := tx.QueryRowContext(ctx, `SELECT directory,scope,workspace_root FROM catalog_sessions WHERE path=?`, path).
		Scan(&target.Path, &target.Scope, &target.WorkspaceRoot); err != nil {
		_ = tx.Rollback()
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if valid {
		_, err = tx.ExecContext(ctx, `UPDATE catalog_sessions SET preview=?,turns=?,turns_state='valid',
            health='ok',meta_fingerprint=? WHERE path=?`,
			preview, turns, fileFingerprint(agent.BranchMetaPath(path)), path)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE catalog_sessions SET turns_state='corrupt',health='corrupt' WHERE path=?`, path)
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
		FROM catalog_sessions WHERE directory=? AND missing_since=0 AND turns_state<>'unknown'`, directory)
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
		known[path] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range records {
		state, ok := known[records[i].Path]
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
