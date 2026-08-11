package sessioncatalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

// RemoveSession records a tombstone before any mutex wait so archived paths
// stop being queryable immediately. SQLite deletion retries asynchronously.
func (c *Catalog) RemoveSession(ctx context.Context, path, reason string) error {
	if c == nil || c.db == nil {
		return nil
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil
	}
	// Immediate query overlay: ListTopics/ListSessions/GetSession filter this
	// map even when the durable DELETE has not committed yet.
	c.removedPaths.Store(path, struct{}{})
	c.writeMu.Lock()
	delete(c.writeQueued, path)
	c.writeMu.Unlock()
	c.pathQueued.Delete(path)
	c.repairQueued.Delete(path)
	// Wake listeners without SQLite. Equal revision identifies an overlay change;
	// empty roots refresh every expanded folder without querying the busy DB for
	// workspace_root.
	if c.opts.OnRevision != nil {
		c.opts.OnRevision(c.revision.Load(), []string{}, reason)
	}

	if err := c.tryApplySessionRemoval(ctx, path, reason); err != nil {
		c.scheduleSessionRemovalRetry(path, reason)
		// Overlay already hides the row. Busy locks and short caller contexts
		// are not interactive failures — durable DELETE is retried in the
		// background.
		if errors.Is(err, errSessionRemovalBusy) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return err
	}
	return nil
}

var errSessionRemovalBusy = errors.New("session catalog removal busy")

func (c *Catalog) scheduleSessionRemovalRetry(path, reason string) {
	if c == nil || c.workerCtx == nil {
		return
	}
	c.workers.Go(func() {
		select {
		case <-c.stop:
			return
		case <-c.workerCtx.Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
		ctx, cancel := context.WithTimeout(c.workerCtx, 5*time.Second)
		defer cancel()
		// Re-check tombstone: a recreate may have cleared it.
		if _, removed := c.removedPaths.Load(path); !removed {
			return
		}
		// Blocking apply is fine on the background worker.
		_ = c.applySessionRemovalLocked(ctx, path, reason+"-retry", true)
	})
}

// tryApplySessionRemoval attempts a non-blocking durable delete. When directory
// or mutation locks are held by reconcile/write, returns errSessionRemovalBusy
// so the caller can keep the tombstone overlay and retry asynchronously.
func (c *Catalog) tryApplySessionRemoval(ctx context.Context, path, reason string) error {
	return c.applySessionRemovalLocked(ctx, path, reason, false)
}

// applySessionRemovalLocked performs the durable SQLite delete. When blocking
// is false, TryLock is used so interactive RemoveSession never waits on mutexes
// after the tombstone is already query-visible.
func (c *Catalog) applySessionRemovalLocked(ctx context.Context, path, reason string, blocking bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Serialize an authoritative removal with directory reconciliation. Without
	// this boundary a scan that captured the old path just before an archive
	// could clear the tombstone and reinsert the stale projection afterwards.
	directoryLock := c.directoryLock(filepath.Dir(path))
	if blocking {
		directoryLock.Lock()
	} else if !directoryLock.TryLock() {
		return errSessionRemovalBusy
	}
	defer directoryLock.Unlock()
	if blocking {
		c.mutationMu.Lock()
	} else if !c.mutationMu.TryLock() {
		return errSessionRemovalBusy
	}
	defer c.mutationMu.Unlock()
	// Prefer a live worker context when the caller deadline already expired
	// (desktop RemoveSession uses ~150ms).
	sqlCtx := ctx
	var cancel context.CancelFunc
	if ctx.Err() != nil && c.workerCtx != nil {
		sqlCtx, cancel = context.WithTimeout(c.workerCtx, 5*time.Second)
		defer cancel()
	}
	tx, err := c.db.BeginTx(sqlCtx, nil)
	if err != nil {
		return err
	}
	var key TopicKey
	err = tx.QueryRowContext(sqlCtx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions WHERE path=?`, path).
		Scan(&key.Scope, &key.WorkspaceRoot, &key.TopicID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(sqlCtx, `DELETE FROM catalog_sessions WHERE path=?`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	if key.TopicID != "" {
		if err := recomputeTopic(sqlCtx, tx, key); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	revision, err := bumpRevision(sqlCtx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publishRevision(revision, []string{key.WorkspaceRoot}, reason)
	c.refreshCounts(sqlCtx)
	return nil
}

func (c *Catalog) pathRemoved(path string) bool {
	if c == nil {
		return false
	}
	_, removed := c.removedPaths.Load(filepath.Clean(path))
	return removed
}
