package sessioncatalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/projectiondb"
)

func (c *Catalog) ReconcileDirectory(ctx context.Context, target DirectoryTarget) error {
	if c == nil || c.db == nil {
		return nil
	}
	target.Path = filepath.Clean(strings.TrimSpace(target.Path))
	if target.Path == "." || target.Path == "" {
		return nil
	}
	lock := c.directoryLock(target.Path)
	lock.Lock()
	defer lock.Unlock()
	target.Scope, target.WorkspaceRoot = normalizeScope(target.Scope, target.WorkspaceRoot)
	signature, err := directorySignature(target.Path)
	if err != nil {
		c.failDirectoryScan(ctx, target.Path, err)
		return err
	}
	if unchanged, err := c.directoryScanCanSkip(ctx, target.Path, signature); err != nil {
		return err
	} else if unchanged {
		return nil
	}
	now := c.opts.Now().UnixMilli()
	generation, resumeCursor, err := c.beginDirectoryScan(ctx, target, signature, now)
	if err != nil {
		return err
	}
	ordered, err := agent.ListSessionOrder(target.Path)
	if err != nil {
		c.failDirectoryScan(ctx, target.Path, err)
		return err
	}
	start := 0
	if resumeCursor != "" {
		for i, info := range ordered {
			if info.Path == resumeCursor {
				start = i + 1
				break
			}
		}
	}
	for ; start < len(ordered); start += 64 {
		if err := ctx.Err(); err != nil {
			// Preserve scan_cursor so the next process resumes instead of
			// re-decoding the whole directory after a forced shutdown.
			c.failDirectoryScan(context.Background(), target.Path, err)
			return err
		}
		end := min(start+64, len(ordered))
		records := make([]SessionRecord, 0, end-start)
		generations := make(map[string]int64, end-start)
		for _, info := range ordered[start:end] {
			record := recordFromOrder(target, info)
			// This scan started while holding the directory lock after any
			// completed removal. A present file is therefore a newer external
			// recreation and may supersede the in-memory removal tombstone.
			c.removedPaths.Delete(record.Path)
			records = append(records, record)
			generations[record.Path] = generation
		}
		if err := c.upsertSessions(ctx, records, generations, "reconcile"); err != nil {
			c.failDirectoryScan(context.Background(), target.Path, err)
			return err
		}
		cursor := ""
		if len(records) > 0 {
			cursor = records[len(records)-1].Path
		}
		c.mutationMu.Lock()
		_, cursorErr := c.db.ExecContext(ctx, `UPDATE catalog_directories SET scan_cursor=?,indexed=? WHERE path=?`, cursor, end, target.Path)
		c.mutationMu.Unlock()
		if cursorErr != nil {
			return cursorErr
		}
		for _, record := range records {
			if record.TurnsState == TurnsUnknown {
				c.enqueueRepair(record.Path)
			}
		}
		runtime.Gosched()
	}
	if err := c.finishDirectoryScan(ctx, target, signature, generation, now, len(ordered)); err != nil {
		return err
	}
	c.refreshCounts(ctx)
	return nil
}

func directorySignature(dir string) (string, error) {
	// os.ReadDir of a plain file returns the file itself on Windows but
	// ENOTDIR on POSIX; stat first so both platforms reject non-directories.
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".meta")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\x00%d\n", name, info.Size(), info.ModTime().UnixNano(), info.Mode())
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (c *Catalog) directoryScanCanSkip(ctx context.Context, path, signature string) (bool, error) {
	var previous, state string
	err := c.db.QueryRowContext(ctx, `SELECT signature,state FROM catalog_directories WHERE path=?`, path).Scan(&previous, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if previous != signature || state != "ready" {
		return false, nil
	}
	var missing int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE directory=? AND missing_since>0`, path).Scan(&missing); err != nil {
		return false, err
	}
	return missing == 0, nil
}

func (c *Catalog) directoryLock(path string) *sync.Mutex {
	c.directoryLocksMu.Lock()
	defer c.directoryLocksMu.Unlock()
	lock := c.directoryLocks[path]
	if lock == nil {
		lock = &sync.Mutex{}
		c.directoryLocks[path] = lock
	}
	return lock
}

// RequestReconcile coalesces external writes by directory. The channel is
// non-blocking so a session save never waits on catalog work; when the channel
// is full the request is retained in reconcileDirty and drained by the worker.
func (c *Catalog) RequestReconcile(target DirectoryTarget) bool {
	if c == nil || strings.TrimSpace(target.Path) == "" {
		return false
	}
	target.Path = filepath.Clean(target.Path)
	if _, loaded := c.reconcileQueued.LoadOrStore(target.Path, target); loaded {
		c.markReconcileDirty(target)
		return true
	}
	select {
	case c.reconcileCh <- target:
		return true
	case <-c.stop:
		c.reconcileQueued.Delete(target.Path)
		return false
	default:
		c.reconcileQueued.Delete(target.Path)
		c.markReconcileDirty(target)
		return false
	}
}

func (c *Catalog) markReconcileDirty(target DirectoryTarget) {
	c.reconcileDirtyMu.Lock()
	c.reconcileDirty[target.Path] = target
	c.reconcileDirtyMu.Unlock()
}

func (c *Catalog) takeReconcileDirty() (DirectoryTarget, bool) {
	c.reconcileDirtyMu.Lock()
	defer c.reconcileDirtyMu.Unlock()
	for path, target := range c.reconcileDirty {
		delete(c.reconcileDirty, path)
		return target, true
	}
	return DirectoryTarget{}, false
}

func (c *Catalog) reconcileLoop() {
	defer c.workers.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if target, ok := c.takeReconcileDirty(); ok {
			c.reconcileQueued.Delete(target.Path)
			ctx, cancel := context.WithTimeout(c.workerCtx, 2*time.Minute)
			_ = c.ReconcileDirectory(ctx, target)
			cancel()
			continue
		}
		select {
		case target := <-c.reconcileCh:
			c.reconcileQueued.Delete(target.Path)
			ctx, cancel := context.WithTimeout(c.workerCtx, 2*time.Minute)
			_ = c.ReconcileDirectory(ctx, target)
			cancel()
		case <-ticker.C:
			// Drain dirty map on the next loop iteration.
		case <-c.stop:
			return
		}
	}
}

// RequestIndexSession coalesces an authoritative session write by path. It is
// intentionally non-blocking so a saturated projection can never delay JSONL
// or sidecar persistence.
func (c *Catalog) RequestIndexSession(target DirectoryTarget, path string) bool {
	if c == nil {
		return false
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return false
	}
	target.Path = filepath.Clean(strings.TrimSpace(target.Path))
	if target.Path == "." || target.Path == "" {
		target.Path = filepath.Dir(path)
	}
	request := sessionPathRequest{target: target, path: path}
	if _, loaded := c.pathQueued.LoadOrStore(path, request); loaded {
		return true
	}
	select {
	case c.pathCh <- request:
		return true
	case <-c.stop:
		c.pathQueued.Delete(path)
		return false
	default:
		c.pathQueued.Delete(path)
		return false
	}
}

func (c *Catalog) sessionPathLoop() {
	defer c.workers.Done()
	for {
		select {
		case request := <-c.pathCh:
			c.pathQueued.Delete(request.path)
			ctx, cancel := context.WithTimeout(c.workerCtx, 30*time.Second)
			_ = c.IndexSessionPath(ctx, request.target, request.path)
			cancel()
		case <-c.stop:
			return
		}
	}
}

// IndexSessionPath prioritizes a restored tab's exact session without walking
// its directory. This keeps cold-start recovery independent from the size of
// the history store.
func (c *Catalog) IndexSessionPath(ctx context.Context, target DirectoryTarget, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil
	}
	target.Path = filepath.Clean(strings.TrimSpace(target.Path))
	if target.Path == "." || target.Path == "" {
		target.Path = filepath.Dir(path)
	}
	// Serialize exact indexing with reconciliation so an older scan cannot mark
	// the new projection missing. Authoritative writes complete before this
	// projection-only lock is acquired.
	lock := c.directoryLock(target.Path)
	lock.Lock()
	defer lock.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Only a path that still exists may supersede a removal tombstone. Doing
	// this after Stat also prevents a delayed exact-index request from exposing
	// an already archived row while its durable DELETE is pending.
	c.removedPaths.Delete(path)
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil {
		return err
	}
	order := agent.SessionOrderInfo{
		Path:           path,
		CreatedAt:      info.ModTime(),
		LastActivityAt: info.ModTime(),
		ModTime:        info.ModTime(),
		Scope:          target.Scope,
		WorkspaceRoot:  target.WorkspaceRoot,
	}
	if ok {
		order.CreatedAt = meta.CreatedAt
		order.LastActivityAt = meta.UpdatedAt
		order.ModTime = meta.UpdatedAt
		order.Scope = meta.DefaultScope()
		order.WorkspaceRoot = meta.WorkspaceRoot
		order.TopicID = meta.TopicID
		order.TopicTitle = meta.TopicTitle
		order.CustomTitle = meta.CustomTitle
		order.Recovered = meta.Recovered
		order.RecoveryReason = meta.RecoveryReason
		order.RecoveryDigest = meta.RecoveryDigest
		order.ParentID = meta.ParentID
		order.Turns = meta.Turns
		order.Preview = meta.Preview
		order.SchemaVersion = meta.SchemaVersion
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = info.ModTime()
	}
	if order.LastActivityAt.IsZero() {
		order.LastActivityAt = info.ModTime()
	}
	record := recordFromOrder(target, order)
	if err := c.UpsertSession(ctx, record); err != nil {
		return err
	}
	if record.TurnsState == TurnsUnknown {
		c.enqueueRepair(record.Path)
	}
	return nil
}

func recordFromOrder(target DirectoryTarget, info agent.SessionOrderInfo) SessionRecord {
	scope, root := normalizeScope(info.Scope, info.WorkspaceRoot)
	if info.TopicID == "" {
		scope, root = target.Scope, target.WorkspaceRoot
	}
	turnsState := TurnsValid
	if info.SchemaVersion < agent.BranchMetaCountsVersion && info.Turns == 0 {
		turnsState = TurnsUnknown
	}
	contentFingerprint := fileFingerprint(info.Path)
	metaFingerprint := fileFingerprint(agent.BranchMetaPath(info.Path))
	createdAt := unixMilli(info.CreatedAt)
	lastActivityAt := unixMilli(info.LastActivityAt)
	// File mtime is a hard floor. Migration/meta can temporarily write zero
	// UpdatedAt; without this, topics sort as inactive and look "missing".
	if st, err := os.Stat(info.Path); err == nil {
		fileMS := st.ModTime().UnixMilli()
		if createdAt <= 0 {
			createdAt = fileMS
		}
		if lastActivityAt <= 0 || fileMS > lastActivityAt {
			lastActivityAt = fileMS
		}
	}
	return normalizeSessionRecord(SessionRecord{
		Path:               info.Path,
		Directory:          target.Path,
		Scope:              scope,
		WorkspaceRoot:      root,
		TopicID:            info.TopicID,
		TopicTitle:         info.TopicTitle,
		CustomTitle:        info.CustomTitle,
		CreatedAt:          createdAt,
		LastActivityAt:     lastActivityAt,
		Preview:            info.Preview,
		Turns:              info.Turns,
		TurnsState:         turnsState,
		Recovered:          info.Recovered,
		RecoveryReason:     info.RecoveryReason,
		RecoveryDigest:     info.RecoveryDigest,
		ParentID:           info.ParentID,
		ContentFingerprint: contentFingerprint,
		MetaFingerprint:    metaFingerprint,
		Health:             HealthOK,
	})
}

func unixMilli(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixMilli()
}

func fileFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// beginDirectoryScan starts or resumes a directory scan. When the previous
// scan for the same signature was interrupted mid-way, the stored scan_cursor
// is returned so ReconcileDirectory continues instead of restarting from 0.
func (c *Catalog) beginDirectoryScan(ctx context.Context, target DirectoryTarget, signature string, now int64) (int64, string, error) {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	var previousSig, previousState, previousCursor string
	var previousGeneration int64
	err = tx.QueryRowContext(ctx, `SELECT signature,state,scan_cursor,scan_generation FROM catalog_directories WHERE path=?`,
		target.Path).Scan(&previousSig, &previousState, &previousCursor, &previousGeneration)
	resume := err == nil && previousState == "scanning" && previousSig == signature && strings.TrimSpace(previousCursor) != ""
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		_ = tx.Rollback()
		return 0, "", err
	}
	if resume {
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_directories SET scope=?,workspace_root=?,state='scanning',error='',signature=? WHERE path=?`,
			target.Scope, target.WorkspaceRoot, signature, target.Path); err != nil {
			_ = tx.Rollback()
			return 0, "", err
		}
		if previousGeneration == 0 {
			previousGeneration = 1
		}
		return previousGeneration, previousCursor, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_directories(path,scope,workspace_root,state,error,signature)
        VALUES(?,?,?,'scanning','',?) ON CONFLICT(path) DO UPDATE SET
        scope=excluded.scope,workspace_root=excluded.workspace_root,state='scanning',error='',
        signature=excluded.signature,scan_generation=catalog_directories.scan_generation+1,scan_cursor='',indexed=0`,
		target.Path, target.Scope, target.WorkspaceRoot, signature); err != nil {
		_ = tx.Rollback()
		return 0, "", err
	}
	var generation int64
	if err := tx.QueryRowContext(ctx, `SELECT scan_generation FROM catalog_directories WHERE path=?`, target.Path).Scan(&generation); err != nil {
		_ = tx.Rollback()
		return 0, "", err
	}
	if generation == 0 {
		generation = 1
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_directories SET scan_generation=1 WHERE path=?`, target.Path); err != nil {
			_ = tx.Rollback()
			return 0, "", err
		}
	}
	return generation, "", tx.Commit()
}

func (c *Catalog) finishDirectoryScan(ctx context.Context, target DirectoryTarget, signature string, generation, now int64, total int) error {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions
        WHERE directory=? AND seen_generation<? AND topic_id<>''`, target.Path, generation)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	affected := map[TopicKey]struct{}{}
	for rows.Next() {
		var key TopicKey
		if err := rows.Scan(&key.Scope, &key.WorkspaceRoot, &key.TopicID); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return err
		}
		affected[key] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_sessions SET
        missing_since=CASE WHEN missing_since=0 THEN ? ELSE missing_since END,
        health='missing'
        WHERE directory=? AND seen_generation<?`, now, target.Path, generation); err != nil {
		_ = tx.Rollback()
		return err
	}
	cutoff := now - c.opts.MissingGrace.Milliseconds()
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_sessions
        WHERE directory=? AND seen_generation<? AND missing_since>0 AND missing_since<=?`, target.Path, generation, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}
	for key := range affected {
		if err := recomputeTopic(ctx, tx, key); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_directories SET state='ready',error='',signature=?,
		scan_cursor='',indexed=?,total=?,completed_at=? WHERE path=?`, signature, total, total, now, target.Path); err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publishRevision(revision, []string{target.WorkspaceRoot}, "reconcile_complete")
	c.refreshCounts(ctx)
	c.statusMu.Lock()
	c.status.State = StateReady
	c.status.LastError = ""
	c.statusMu.Unlock()
	return nil
}

func (c *Catalog) failDirectoryScan(ctx context.Context, path string, scanErr error) {
	c.mutationMu.Lock()
	_, _ = c.db.ExecContext(ctx, `UPDATE catalog_directories SET state='degraded',error=? WHERE path=?`, scanErr.Error(), path)
	c.mutationMu.Unlock()
	c.statusMu.Lock()
	c.status.State = StateDegraded
	c.status.LastError = scanErr.Error()
	c.statusMu.Unlock()
}

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
	ctx, cancel := context.WithTimeout(workerCtx, 30*time.Second)
	defer cancel()
	// LoadSessionDisplayMessages is not yet context-aware; check cancellation
	// before and after so shutdown can abandon the result without writing.
	msgs, _, _, err := agent.LoadSessionDisplayMessages(path)
	if ctx.Err() != nil || workerCtx.Err() != nil {
		return
	}
	if err != nil {
		_ = c.applyRepairResult(ctx, path, "", 0, false, "repair_corrupt")
		return
	}
	preview, turns := agent.SessionPreviewFromMessages(msgs)
	if err := agent.UpdateBranchMeta(path, false, func(meta *agent.BranchMeta) error {
		meta.Preview = preview
		meta.Turns = turns
		meta.SchemaVersion = agent.BranchMetaCountsVersion
		return nil
	}); err != nil {
		return
	}
	if ctx.Err() != nil || workerCtx.Err() != nil {
		return
	}
	_ = c.applyRepairResult(ctx, path, preview, turns, true, "repair")
}

func (c *Catalog) applyRepairResult(ctx context.Context, path, preview string, turns int, valid bool, reason string) error {
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var key TopicKey
	if err := tx.QueryRowContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions WHERE path=?`, path).Scan(&key.Scope, &key.WorkspaceRoot, &key.TopicID); err != nil {
		_ = tx.Rollback()
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if valid {
		if _, err := tx.ExecContext(ctx, `UPDATE catalog_sessions SET preview=?,turns=?,turns_state='valid',
            health='ok',meta_fingerprint=? WHERE path=?`, preview, turns, fileFingerprint(agent.BranchMetaPath(path)), path); err != nil {
			_ = tx.Rollback()
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE catalog_sessions SET turns_state='corrupt',health='corrupt' WHERE path=?`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	if key.TopicID != "" {
		if err := recomputeTopic(ctx, tx, key); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publishRevision(revision, []string{key.WorkspaceRoot}, reason)
	c.refreshCounts(ctx)
	return nil
}

// Rebuild replaces only the disposable catalog. Authoritative sessions and
// sidecars are never changed or removed by this operation. The live database
// stays in place until a fully-populated replacement is validated and swapped.
func Rebuild(ctx context.Context, path string, targets []DirectoryTarget) (Status, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if strings.TrimSpace(path) == "" {
		catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
		if err != nil {
			return Status{}, err
		}
		for _, target := range targets {
			if err := catalog.ReconcileDirectory(ctx, target); err != nil {
				closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = catalog.Close(closeCtx)
				cancel()
				return catalog.Status(), err
			}
		}
		status := catalog.Status()
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = catalog.Close(closeCtx)
		cancel()
		return status, nil
	}
	err := projectiondb.Rebuild(ctx, projectiondb.OpenOptions{
		Path:       path,
		MemoryName: "session-catalog-rebuild",
		Migrations: sessionMigrations(),
	}, func(ctx context.Context, db *sql.DB) error {
		// Populate through a catalog that owns this temporary database handle
		// without starting background repair workers.
		temp := &Catalog{
			db:             db,
			opts:           Options{Path: path, DisableRepair: true, Now: time.Now, MissingGrace: defaultMissingGrace},
			writeQueued:    map[string]SessionRecord{},
			directoryLocks: map[string]*sync.Mutex{},
			stop:           make(chan struct{}),
			status:         Status{State: StateReady, Mode: ModeDisk, Path: path},
		}
		temp.workerCtx, temp.workerCancel = context.WithCancel(ctx)
		defer temp.workerCancel()
		for _, target := range targets {
			if err := temp.ReconcileDirectory(ctx, target); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	// Open the published replacement briefly for a status snapshot, then close.
	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		return Status{State: StateReady, Mode: ModeDisk, Path: path}, nil
	}
	status := catalog.Status()
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = catalog.Close(closeCtx)
	cancel()
	return status, nil
}

// Inspect is read-only. It never migrates, repairs, quarantines, or rewrites a
// catalog, making it suitable for `reasonix doctor sessions`.
func Inspect(ctx context.Context, path string) (Status, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	status := Status{State: StateDegraded, Mode: ModeDisk, Path: path}
	if strings.TrimSpace(path) == "" {
		status.LastError = "catalog path unavailable"
		return status, nil
	}
	inspection := projectiondb.Inspect(ctx, path)
	if inspection.Error != "" && !inspection.Exists {
		status.LastError = inspection.Error
		return status, nil
	}
	if !inspection.Exists {
		status.LastError = "catalog does not exist"
		return status, nil
	}
	if inspection.Integrity != "" && inspection.Integrity != "ok" {
		status.LastError = inspection.Integrity
		return status, nil
	}
	if inspection.Error != "" {
		status.LastError = inspection.Error
		return status, nil
	}
	u := &url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout%28150%29")
	if err != nil {
		return status, err
	}
	defer db.Close()
	status.State = StateReady
	_ = db.QueryRowContext(ctx, `SELECT revision FROM catalog_state WHERE id=1`).Scan(&status.Revision)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions`).Scan(&status.Indexed)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE turns_state='unknown'`).Scan(&status.RepairPending)
	return status, nil
}
