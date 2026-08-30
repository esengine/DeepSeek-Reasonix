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
	if unchanged, err := c.directoryScanCanSkip(ctx, target, signature); err != nil {
		return err
	} else if unchanged {
		return nil
	}
	now := c.opts.Now().UnixMilli()
	generation, _, err := c.beginDirectoryScan(ctx, target, signature, now)
	if err != nil {
		return err
	}
	ordered, err := agent.ListSessionOrder(target.Path)
	if err != nil {
		c.failDirectoryScan(ctx, target.Path, err)
		return err
	}
	records := make([]SessionRecord, 0, len(ordered))
	for start := 0; start < len(ordered); start += 64 {
		if err := ctx.Err(); err != nil {
			c.failDirectoryScan(context.Background(), target.Path, err)
			return err
		}
		end := min(start+64, len(ordered))
		for _, info := range ordered[start:end] {
			record := recordFromOrder(target, info)
			// This scan started while holding the directory lock after any
			// completed removal. A present file is therefore a newer external
			// recreation and may supersede the in-memory removal tombstone.
			c.removedPaths.Delete(record.Path)
			records = append(records, record)
		}
		runtime.Gosched()
	}
	records, err = c.preserveKnownSourceStates(ctx, target.Path, records)
	if err != nil {
		c.failDirectoryScan(context.Background(), target.Path, err)
		return err
	}
	for i := range records {
		records[i] = classifyRecoveryLineageFromContent(normalizeSessionRecord(records[i]))
	}
	records = promoteCanonicalLeaves(records)
	if err := c.commitDirectoryProjection(ctx, target, signature, generation, now, records); err != nil {
		c.failDirectoryScan(context.Background(), target.Path, err)
		return err
	}
	for _, record := range records {
		if record.TurnsState == TurnsUnknown {
			c.enqueueRepair(record.Path)
		}
	}
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

// IndexSessionPath indexes one session without walking its directory.
func (c *Catalog) IndexSessionPath(ctx context.Context, target DirectoryTarget, path string) error {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil
	}
	target.Path = filepath.Clean(strings.TrimSpace(target.Path))
	if target.Path == "." || target.Path == "" {
		target.Path = filepath.Dir(path)
	}
	// Hold the directory lock so a concurrent scan cannot mark this row missing.
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
		order.RecoveryPreferred = agent.RecoveryPreferenceCurrent(path, meta)
		order.Turns = meta.Turns
		order.Preview = meta.Preview
		order.SchemaVersion = meta.SchemaVersion
		order.Revision = meta.Revision
		order.ContentDigest = meta.ContentDigest
		order.ListingRevision = meta.ListingRevision
		order.ListingContentDigest = meta.ListingContentDigest
	}
	if order.CreatedAt.IsZero() {
		order.CreatedAt = info.ModTime()
	}
	if order.LastActivityAt.IsZero() {
		order.LastActivityAt = info.ModTime()
	}
	record := recordFromOrder(target, order)
	projectionDirty, err := c.upsertExactPathSession(ctx, record)
	if err != nil {
		return err
	}
	if projectionDirty {
		// Queue after the exact source row is durable. The non-blocking worker
		// will acquire this directory lock after IndexSessionPath returns and
		// publish the full sibling-aware projection in one transaction.
		c.RequestReconcile(target)
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
	if !info.ListingProjectionFresh() {
		turnsState, info.Preview, info.Turns = TurnsUnknown, "", 0
	}
	contentFingerprint := fileFingerprint(info.Path)
	metaFingerprint := fileFingerprint(agent.BranchMetaPath(info.Path))
	createdAt := unixMilli(info.CreatedAt)
	lastActivityAt := unixMilli(info.LastActivityAt)
	// File mtime fills a missing clock. Do not raise a known sidecar UpdatedAt:
	// repair and other metadata writes bump mtime without new user turns.
	if st, err := os.Stat(info.Path); err == nil {
		fileMS := st.ModTime().UnixMilli()
		if createdAt <= 0 {
			createdAt = fileMS
		}
		if lastActivityAt <= 0 {
			lastActivityAt = fileMS
		}
	}
	// Real content only; failure/missing parent leaves RecoveryCopy false.
	recoveryCopy := info.Recovered && agent.RecoveryBranchCoveredByParent(info.Path, target.Path)
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
		RecoveryPreferred:  info.RecoveryPreferred,
		RecoveryCopy:       recoveryCopy,
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

// commitDirectoryProjection publishes a complete sibling-aware directory
// snapshot. Parsing and lineage classification happen before this function;
// readers therefore observe either the previous committed projection or every
// row, tombstone, missing marker, topic aggregate, and readiness update from
// this transaction together.
func (c *Catalog) commitDirectoryProjection(ctx context.Context, target DirectoryTarget, signature string, generation, now int64, records []SessionRecord) error {
	c.mutationMu.Lock()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		c.mutationMu.Unlock()
		return err
	}
	rollback := func(commitErr error) error {
		_ = tx.Rollback()
		c.mutationMu.Unlock()
		return commitErr
	}
	stmt, err := tx.PrepareContext(ctx, sessionInsertSQL+directoryProjectionUpdateSQL)
	if err != nil {
		return rollback(err)
	}
	affected := map[TopicKey]struct{}{}
	for start := 0; start < len(records); start += 64 {
		if err := ctx.Err(); err != nil {
			_ = stmt.Close()
			return rollback(err)
		}
		end := min(start+64, len(records))
		for _, record := range records[start:end] {
			var previous TopicKey
			if err := tx.QueryRowContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions WHERE path=?`, record.Path).
				Scan(&previous.Scope, &previous.WorkspaceRoot, &previous.TopicID); err == nil && previous.TopicID != "" {
				affected[previous] = struct{}{}
			} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
				_ = stmt.Close()
				return rollback(err)
			}
			if _, err := stmt.ExecContext(ctx, sessionRowValues(record, generation)...); err != nil {
				_ = stmt.Close()
				return rollback(err)
			}
			if record.TopicID != "" {
				affected[TopicKey{Scope: record.Scope, WorkspaceRoot: record.WorkspaceRoot, TopicID: record.TopicID}] = struct{}{}
			}
			if err := updateFoldedTopicTombstones(ctx, tx, previous, record, now); err != nil {
				_ = stmt.Close()
				return rollback(err)
			}
		}
		if c.testReconcileBatchHook != nil {
			c.testReconcileBatchHook(end)
		}
		runtime.Gosched()
	}
	if err := stmt.Close(); err != nil {
		return rollback(err)
	}

	rows, err := tx.QueryContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions
        WHERE directory=? AND seen_generation<? AND topic_id<>''`, target.Path, generation)
	if err != nil {
		return rollback(err)
	}
	for rows.Next() {
		var key TopicKey
		if err := rows.Scan(&key.Scope, &key.WorkspaceRoot, &key.TopicID); err != nil {
			_ = rows.Close()
			return rollback(err)
		}
		affected[key] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_sessions SET
        missing_since=CASE WHEN missing_since=0 THEN ? ELSE missing_since END,
        health='missing'
        WHERE directory=? AND seen_generation<?`, now, target.Path, generation); err != nil {
		return rollback(err)
	}
	cutoff := now - c.opts.MissingGrace.Milliseconds()
	if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_sessions
        WHERE directory=? AND seen_generation<? AND missing_since>0 AND missing_since<=?`, target.Path, generation, cutoff); err != nil {
		return rollback(err)
	}
	for key := range affected {
		if err := recomputeTopic(ctx, tx, key); err != nil {
			return rollback(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_directories SET state='ready',error='',signature=?,
		scan_cursor='',indexed=?,total=?,completed_at=? WHERE path=?`, signature, len(records), len(records), now, target.Path); err != nil {
		return rollback(err)
	}
	revision, err := bumpRevision(ctx, tx)
	if err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		c.mutationMu.Unlock()
		return err
	}
	c.mutationMu.Unlock()

	c.publishRevision(revision, []string{target.WorkspaceRoot}, "reconcile_complete")
	c.refreshCounts(ctx)
	c.statusMu.Lock()
	c.status.State = StateReady
	c.status.LastError = ""
	c.statusMu.Unlock()
	return nil
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

// Rebuild replaces only the disposable catalog. Authoritative sessions and
// sidecars are never changed or removed by this operation. The live database
// stays in place until a fully-populated replacement is validated and swapped.
func Rebuild(ctx context.Context, path string, targets []DirectoryTarget) (Status, error) {
	return RebuildWithRevisionFloor(ctx, path, targets, 0)
}

// RebuildWithRevisionFloor preserves the caller's revision epoch while
// atomically replacing the disposable projection. Desktop clients retain
// revision fences across the rebuild, so a replacement must never publish a
// lower revision than the catalog they already rendered.
func RebuildWithRevisionFloor(ctx context.Context, path string, targets []DirectoryTarget, revisionFloor uint64) (Status, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath()
	}
	if strings.TrimSpace(path) == "" {
		catalog, err := Open(ctx, Options{InMemory: true, DisableRepair: true})
		if err != nil {
			return Status{}, err
		}
		if err := setCatalogRevisionFloor(ctx, catalog.db, revisionFloor); err != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = catalog.Close(closeCtx)
			cancel()
			return Status{}, err
		}
		catalog.rememberRevision(revisionFloor)
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
		Path:         path,
		MemoryName:   "session-catalog-rebuild",
		Migrations:   sessionMigrations(),
		RetainBackup: true,
	}, func(ctx context.Context, db *sql.DB) error {
		if err := setCatalogRevisionFloor(ctx, db, revisionFloor); err != nil {
			return err
		}
		// Populate through a catalog that owns this temporary database handle
		// without starting background repair workers.
		temp := &Catalog{
			db:             db,
			opts:           Options{Path: path, DisableRepair: true, Now: time.Now, MissingGrace: defaultMissingGrace},
			writeQueued:    map[string]SessionRecord{},
			directoryLocks: map[string]*sync.Mutex{},
			stop:           make(chan struct{}),
			status:         Status{State: StateReady, Mode: ModeDisk, Path: path, Revision: revisionFloor},
		}
		temp.revision.Store(revisionFloor)
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
		return Status{State: StateReady, Mode: ModeDisk, Path: path, Revision: revisionFloor}, nil
	}
	status := catalog.Status()
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = catalog.Close(closeCtx)
	cancel()
	return status, nil
}

func setCatalogRevisionFloor(ctx context.Context, db *sql.DB, revisionFloor uint64) error {
	if db == nil || revisionFloor == 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, `UPDATE catalog_state SET revision=? WHERE id=1 AND revision<?`, revisionFloor, revisionFloor)
	return err
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
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE missing_since=0`).Scan(&status.PhysicalSessions)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_topics`).Scan(&status.LogicalSessions)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT recovery_group_id) FROM catalog_sessions WHERE recovered=1 AND recovery_group_id<>'' AND missing_since=0`).Scan(&status.RecoveryGroups)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND missing_since=0`).Scan(&status.RecoveryBranches)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND recovery_role='diverged' AND missing_since=0`).Scan(&status.RecoveryDiverged)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND recovery_role='covered_copy' AND missing_since=0`).Scan(&status.CleanupEligible)
	return status, nil
}
