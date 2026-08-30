package historycatalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/projectiondb"
	"reasonix/internal/retrieval"
	"reasonix/internal/store"
)

const defaultMissingGrace = 30 * time.Second

type Catalog struct {
	db         *sql.DB
	opts       Options
	revision   atomic.Uint64
	statusMu   sync.RWMutex
	status     Status
	ctx        context.Context
	cancel     context.CancelFunc
	queue      chan string
	rootCh     chan string
	flushCh    chan chan struct{}
	mu         sync.Mutex
	paths      map[string]queuedPath
	roots      map[string]Root
	dirtyRoots map[string]bool
	wg         sync.WaitGroup
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
}

type queuedPath struct {
	root       Root
	appendFrom int
}

func Open(ctx context.Context, opts Options) (*Catalog, error) {
	if opts.Path == "" {
		opts.Path = DefaultPath()
	}
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = ""
		opts.InMemory = true
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 1024
	}
	if opts.MissingGrace <= 0 {
		opts.MissingGrace = defaultMissingGrace
	}
	if opts.ReconcileInterval <= 0 {
		// Periodic root rescans are fingerprint-cheap when nothing changed; keep
		// the interval longer so large installs are not re-walked every minute.
		opts.ReconcileInterval = 5 * time.Minute
	}
	opts.MaxBytes = resolveMaxBytes(opts.MaxBytes, configuredMaxMB())
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: opts.Path, MemoryName: "history-search", Migrations: migrations(), InMemory: opts.InMemory,
		MaxOpenConns: 4, Now: opts.Now, SecureDelete: true, AutoVacuum: true,
	})
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	c := &Catalog{db: handle.DB, opts: opts, ctx: workerCtx, cancel: cancel,
		queue: make(chan string, opts.QueueCapacity), rootCh: make(chan string, 64),
		flushCh: make(chan chan struct{}, 1),
		paths:   map[string]queuedPath{}, roots: map[string]Root{}, dirtyRoots: map[string]bool{},
		closeDone: make(chan struct{}),
		status: Status{State: string(handle.Status.State), Mode: handle.Status.Mode, Path: handle.Status.Path,
			LastError: handle.Status.LastError, QuarantinedPath: handle.Status.QuarantinedPath}}
	if err := c.db.QueryRowContext(ctx, `SELECT revision FROM history_state WHERE id=1`).Scan(new(uint64)); err != nil {
		_ = c.db.Close()
		cancel()
		return nil, err
	}
	if err := c.ensureTokenizerVersion(ctx); err != nil {
		_ = c.db.Close()
		cancel()
		return nil, err
	}
	var revision uint64
	_ = c.db.QueryRowContext(ctx, `SELECT revision FROM history_state WHERE id=1`).Scan(&revision)
	c.revision.Store(revision)
	c.refreshStatus(ctx)
	c.wg.Add(1)
	go c.worker()
	// A far-over-cap index from before the cap existed (#8717) is cheaper to
	// rebuild than to evict session-by-session; wipe async so startup never
	// blocks. Registered roots rescan afterwards and re-index truncated.
	if !opts.InMemory && strings.TrimSpace(opts.Path) != "" &&
		historyDBFileSize(opts.Path) > rebuildOversizeFactor*opts.MaxBytes {
		c.wg.Go(func() {
			c.wipeForRebuild(c.ctx)
		})
	}
	return c, nil
}

func (c *Catalog) ensureTokenizerVersion(ctx context.Context) error {
	var version int
	if err := c.db.QueryRowContext(ctx, `SELECT tokenizer_version FROM history_state WHERE id=1`).Scan(&version); err != nil {
		return err
	}
	if version == TokenizerVersion {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := wipeProjectionRows(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE history_state SET tokenizer_version=?,revision=revision+1 WHERE id=1`, TokenizerVersion); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *Catalog) RegisterRoot(root Root) bool {
	if c == nil || strings.TrimSpace(root.Path) == "" {
		return false
	}
	root.Path = filepath.Clean(root.Path)
	if root.Scope != "project" {
		root.Scope = "global"
		root.WorkspaceRoot = ""
	}
	c.mu.Lock()
	_, alreadyRegistered := c.roots[root.Path]
	c.roots[root.Path] = root
	c.mu.Unlock()
	if !alreadyRegistered {
		c.statusMu.Lock()
		c.status.Pending++
		c.statusMu.Unlock()
	}
	select {
	case c.rootCh <- root.Path:
		return true
	default:
		c.markRootDirty(root.Path)
		return false
	}
}

// ReconcileRoot performs one deterministic scan. Production callers normally
// use RegisterRoot; the synchronous form exists for doctor/reindex and tests.
func (c *Catalog) ReconcileRoot(ctx context.Context, root Root) error {
	root.Path = filepath.Clean(root.Path)
	if root.Scope != "project" {
		root.Scope = "global"
		root.WorkspaceRoot = ""
	}
	return c.reconcileRoot(ctx, root)
}

func (c *Catalog) EnqueuePath(root Root, path string) bool {
	return c.enqueuePath(root, path, -1)
}

func (c *Catalog) EnqueuePersist(root Root, event agent.SessionPersistEvent) bool {
	appendFrom := event.AppendFrom
	if event.Rewrite {
		appendFrom = -1
	}
	return c.enqueuePath(root, event.Path, appendFrom)
}

func (c *Catalog) enqueuePath(root Root, path string, appendFrom int) bool {
	if c == nil || strings.TrimSpace(path) == "" {
		return false
	}
	path = filepath.Clean(path)
	c.mu.Lock()
	if queued, exists := c.paths[path]; exists {
		queued.root = root
		if queued.appendFrom < 0 || appendFrom < 0 {
			queued.appendFrom = -1
		} else if appendFrom < queued.appendFrom {
			queued.appendFrom = appendFrom
		}
		c.paths[path] = queued
		c.mu.Unlock()
		return true
	}
	c.paths[path] = queuedPath{root: root, appendFrom: appendFrom}
	c.mu.Unlock()
	select {
	case c.queue <- path:
		return true
	default:
		c.mu.Lock()
		delete(c.paths, path)
		c.dirtyRoots[root.Path] = true
		c.mu.Unlock()
		return false
	}
}

// EnqueueExisting prioritizes a source already known to the catalog without
// requiring the caller to retain its root metadata.
func (c *Catalog) EnqueueExisting(ctx context.Context, path string) bool {
	var root Root
	err := c.db.QueryRowContext(ctx, `SELECT root,source,scope,workspace_root FROM history_sources WHERE path=?`, filepath.Clean(path)).Scan(
		&root.Path, &root.Source, &root.Scope, &root.WorkspaceRoot)
	if err != nil {
		return false
	}
	return c.EnqueuePath(root, path)
}

func (c *Catalog) worker() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.opts.ReconcileInterval)
	defer ticker.Stop()
	for {
		if root, ok := c.takeDirtyRoot(); ok {
			_ = c.reconcileRoot(c.ctx, root)
			continue
		}
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.markAllRootsDirty()
			c.governSize(c.ctx)
		case done := <-c.flushCh:
			c.drainPending(c.ctx)
			close(done)
		case path := <-c.queue:
			c.mu.Lock()
			queued := c.paths[path]
			delete(c.paths, path)
			c.mu.Unlock()
			_ = c.indexPath(c.ctx, queued.root, path, 0, queued.appendFrom)
		case path := <-c.rootCh:
			c.mu.Lock()
			root, ok := c.roots[path]
			c.mu.Unlock()
			if ok {
				_ = c.reconcileRoot(c.ctx, root)
			}
		}
	}
}

// Flush drains dirty roots and the path queue until empty or ctx cancels, then
// waits for the worker to acknowledge. Callers use this on shutdown so pending
// index work is not silently abandoned.
func (c *Catalog) Flush(ctx context.Context) error {
	if c == nil {
		return nil
	}
	done := make(chan struct{})
	select {
	case c.flushCh <- done:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Catalog) drainPending(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		if root, ok := c.takeDirtyRoot(); ok {
			_ = c.reconcileRoot(ctx, root)
			continue
		}
		select {
		case path := <-c.queue:
			c.mu.Lock()
			queued := c.paths[path]
			delete(c.paths, path)
			c.mu.Unlock()
			_ = c.indexPath(ctx, queued.root, path, 0, queued.appendFrom)
		case path := <-c.rootCh:
			c.mu.Lock()
			root, ok := c.roots[path]
			c.mu.Unlock()
			if ok {
				_ = c.reconcileRoot(ctx, root)
			}
		default:
			c.mu.Lock()
			empty := len(c.paths) == 0 && len(c.dirtyRoots) == 0
			c.mu.Unlock()
			if empty && len(c.queue) == 0 && len(c.rootCh) == 0 {
				return
			}
			// Another goroutine may have enqueued between checks; yield once.
			runtime.Gosched()
			c.mu.Lock()
			empty = len(c.paths) == 0 && len(c.dirtyRoots) == 0
			c.mu.Unlock()
			if empty && len(c.queue) == 0 && len(c.rootCh) == 0 {
				return
			}
		}
	}
}

func (c *Catalog) markRootDirty(path string) {
	c.mu.Lock()
	c.dirtyRoots[path] = true
	c.mu.Unlock()
}

func (c *Catalog) markAllRootsDirty() {
	c.mu.Lock()
	for path := range c.roots {
		c.dirtyRoots[path] = true
	}
	c.mu.Unlock()
}

func (c *Catalog) takeDirtyRoot() (Root, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.dirtyRoots {
		delete(c.dirtyRoots, path)
		root, ok := c.roots[path]
		return root, ok
	}
	return Root{}, false
}

func historyRootSignature(paths []string) string {
	hash := sha256.New()
	for _, path := range paths {
		for _, candidate := range []string{path, agent.BranchMetaPath(path)} {
			info, err := os.Stat(candidate)
			if err != nil {
				_, _ = fmt.Fprintf(hash, "%s\x00missing\n", candidate)
				continue
			}
			_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%d\n", candidate, info.Size(), info.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *Catalog) reconcileRoot(ctx context.Context, root Root) error {
	entries, err := os.ReadDir(root.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.setError(err)
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !store.IsSessionTranscriptName(entry.Name()) {
			continue
		}
		path := filepath.Join(root.Path, entry.Name())
		if !root.Archive && !agent.IsVisibleSession(path) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	signature := historyRootSignature(paths)
	var previousSig, previousState string
	_ = c.db.QueryRowContext(ctx, `SELECT signature,state FROM history_roots WHERE path=?`, root.Path).Scan(&previousSig, &previousState)
	if previousState == "ready" && previousSig == signature && signature != "" {
		return nil
	}
	now := c.opts.Now().UnixMilli()
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var generation int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO history_roots(path,source,scope,workspace_root,signature,scan_generation,state,total)
        VALUES(?,?,?,?,?,1,'scanning',?) ON CONFLICT(path) DO UPDATE SET source=excluded.source,scope=excluded.scope,
        workspace_root=excluded.workspace_root,signature=excluded.signature,scan_generation=history_roots.scan_generation+1,state='scanning',error='',total=excluded.total
        RETURNING scan_generation`, root.Path, root.Source, root.Scope, root.WorkspaceRoot, signature, len(paths)).Scan(&generation); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	indexed := 0
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.indexPath(ctx, root, path, generation, -1); err == nil {
			indexed++
		}
		if (i+1)%32 == 0 {
			runtime.Gosched()
		}
	}
	tx, err = c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE history_sources SET missing_since=CASE WHEN missing_since=0 THEN ? ELSE missing_since END,
        health='missing' WHERE root=? AND seen_generation<>?`, now, root.Path, generation); err != nil {
		_ = tx.Rollback()
		return err
	}
	cutoff := now - c.opts.MissingGrace.Milliseconds()
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_fts WHERE rowid IN (
        SELECT d.id FROM history_documents d JOIN history_sources s ON s.path=d.source_path
        WHERE s.root=? AND s.seen_generation<>? AND s.missing_since>0 AND s.missing_since<=?
    )`, root.Path, generation, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_documents WHERE source_path IN (
        SELECT path FROM history_sources WHERE root=? AND seen_generation<>? AND missing_since>0 AND missing_since<=?
    )`, root.Path, generation, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_sources
        WHERE root=? AND seen_generation<>? AND missing_since>0 AND missing_since<=?`, root.Path, generation, cutoff); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE history_roots SET state='ready',signature=?,indexed=?,completed_at=?,scan_cursor='' WHERE path=?`, signature, indexed, now, root.Path); err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publish(revision, []string{root.Path}, "reconcile")
	return nil
}

func fileFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

func (c *Catalog) indexPath(ctx context.Context, root Root, path string, generation int64, appendFrom int) error {
	if !root.Archive && !agent.IsVisibleSession(path) {
		return c.Purge(ctx, path)
	}
	contentFingerprint := fileFingerprint(path)
	metaFingerprint := fileFingerprint(agent.BranchMetaPath(path))
	state, known, identityErr := agent.SessionContentIdentity(path)
	digest := ""
	revision := int64(0)
	if identityErr == nil && known {
		digest, revision = state.DigestHex, state.Revision
	}
	var oldFingerprint, oldMetaFingerprint, oldDigest, oldHealth string
	var oldGeneration, oldRevision int64
	var oldMessageCount int
	err := c.db.QueryRowContext(ctx, `SELECT content_fingerprint,meta_fingerprint,content_digest,seen_generation,
		content_revision,indexed_message_count,health FROM history_sources WHERE path=?`, path).Scan(
		&oldFingerprint, &oldMetaFingerprint, &oldDigest, &oldGeneration, &oldRevision, &oldMessageCount, &oldHealth)
	if sourceProjectionUnchanged(err, oldFingerprint, contentFingerprint, oldMetaFingerprint, metaFingerprint, oldDigest, digest) {
		if generation != 0 && oldGeneration != generation {
			// Keep evicted rows evicted: an unchanged file must not re-enter the index.
			_, _ = c.db.ExecContext(ctx, `UPDATE history_sources SET seen_generation=?,missing_since=0,
				health=CASE WHEN health='evicted' THEN 'evicted' ELSE 'ok' END WHERE path=?`, generation, path)
		}
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// An evicted projection has no prefix to append onto; fall through to a full reload.
	if err == nil && known && appendFrom >= 0 && oldHealth != "evicted" {
		handled, appendErr := c.tryAppendPath(ctx, root, path, generation, appendFrom, oldMessageCount, oldRevision,
			revision, digest, contentFingerprint, metaFingerprint)
		if appendErr != nil {
			return appendErr
		}
		if handled {
			return nil
		}
	}
	session, err := agent.LoadSession(path)
	if err != nil {
		_, _ = c.db.ExecContext(ctx, `INSERT INTO history_sources(path,root,source,scope,workspace_root,content_fingerprint,meta_fingerprint,health,last_error,seen_generation)
            VALUES(?,?,?,?,?,?,?,'corrupt',?,?) ON CONFLICT(path) DO UPDATE SET health='corrupt',last_error=excluded.last_error,
            content_fingerprint=excluded.content_fingerprint,meta_fingerprint=excluded.meta_fingerprint,seen_generation=excluded.seen_generation`,
			path, root.Path, root.Source, root.Scope, root.WorkspaceRoot, contentFingerprint, metaFingerprint, err.Error(), generation)
		c.setError(err)
		return err
	}
	messages := session.Snapshot()
	if digest == "" {
		h := sha256.New()
		for _, doc := range documents(messages) {
			_, _ = h.Write([]byte(doc.terms))
			_, _ = h.Write([]byte{0})
		}
		digest = hex.EncodeToString(h.Sum(nil))
	}
	meta, _, _ := agent.LoadBranchMeta(path)
	lastActivity := max(int64(0), agent.SessionContentModTime(path).UnixMilli())
	// Hide stale terms as soon as the authoritative fingerprint changes. Rows
	// remain available for retry and are atomically replaced below.
	if _, err := c.db.ExecContext(ctx, `UPDATE history_sources SET health='stale',last_error='' WHERE path=?`, path); err != nil {
		return err
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_fts WHERE rowid IN (SELECT id FROM history_documents WHERE source_path=?)`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_documents WHERE source_path=?`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO history_sources(path,root,source,scope,workspace_root,content_revision,content_digest,
        content_fingerprint,meta_fingerprint,message_count,indexed_message_count,custom_title,topic_id,topic_title,preview,created_at,
        last_activity_at,health,missing_since,seen_generation,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'ok',0,?,'')
        ON CONFLICT(path) DO UPDATE SET root=excluded.root,source=excluded.source,scope=excluded.scope,workspace_root=excluded.workspace_root,
        content_revision=excluded.content_revision,content_digest=excluded.content_digest,content_fingerprint=excluded.content_fingerprint,
        meta_fingerprint=excluded.meta_fingerprint,message_count=excluded.message_count,indexed_message_count=excluded.indexed_message_count,
        custom_title=excluded.custom_title,topic_id=excluded.topic_id,topic_title=excluded.topic_title,preview=excluded.preview,
        created_at=excluded.created_at,last_activity_at=excluded.last_activity_at,health='ok',missing_since=0,
        seen_generation=excluded.seen_generation,last_error=''`, path, root.Path, root.Source, root.Scope, root.WorkspaceRoot, revision, digest,
		contentFingerprint, metaFingerprint, len(messages), len(messages), meta.CustomTitle, meta.TopicID, meta.TopicTitle, meta.Preview,
		meta.CreatedAt.UnixMilli(), lastActivity, generation)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, doc := range documents(messages) {
		result, err := tx.ExecContext(ctx, `INSERT INTO history_documents(source_path,message_index,part_index,role,kind,tool_name,token_count)
            VALUES(?,?,?,?,?,?,?)`, path, doc.message, doc.part, doc.role, doc.kind, doc.tool, doc.count)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO history_fts(rowid,terms) VALUES(?,?)`, rowID, doc.terms); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	newRevision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	c.publish(newRevision, []string{root.Path}, "source-indexed")
	return nil
}

func bump(ctx context.Context, tx *sql.Tx) (uint64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE history_state SET revision=revision+1 WHERE id=1`); err != nil {
		return 0, err
	}
	var revision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM history_state WHERE id=1`).Scan(&revision)
	return revision, err
}

func (c *Catalog) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	out := SearchResult{Items: []Candidate{}, Revision: c.revision.Load(), Partial: c.Status().Pending > 0}
	terms, err := retrieval.QueryTerms(req.Query)
	if err != nil {
		return out, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	match := make([]string, 0, len(terms))
	for _, term := range terms {
		match = append(match, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	where := []string{`history_fts MATCH ?`, `s.health='ok'`, `s.missing_since=0`}
	args := []any{strings.Join(match, " OR ")}
	if req.Scope == "project" {
		where = append(where, `s.scope='project'`, `s.workspace_root=?`)
		args = append(args, strings.TrimSpace(req.WorkspaceRoot))
	}
	if len(req.Kinds) > 0 {
		placeholders := make([]string, len(req.Kinds))
		for i, kind := range req.Kinds {
			placeholders[i] = "?"
			args = append(args, kind)
		}
		where = append(where, `d.kind IN (`+strings.Join(placeholders, ",")+`)`)
	}
	if tool := strings.TrimSpace(req.ToolName); tool != "" {
		where = append(where, `d.tool_name=?`)
		args = append(args, tool)
	}
	if len(req.Roots) > 0 {
		placeholders := make([]string, 0, len(req.Roots))
		for _, root := range req.Roots {
			if strings.TrimSpace(root) == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, filepath.Clean(root))
		}
		if len(placeholders) > 0 {
			where = append(where, `s.root IN (`+strings.Join(placeholders, ",")+`)`)
		}
	}
	baseQuery := `SELECT d.id AS id,d.source_path AS source_path,s.root AS root,s.source AS source,s.scope AS scope,
		s.workspace_root AS workspace_root,s.content_digest AS content_digest,d.message_index AS message_index,
		d.part_index AS part_index,d.role AS role,d.kind AS kind,d.tool_name AS tool_name,bm25(history_fts) AS rank,
		s.custom_title AS custom_title,s.topic_title AS topic_title,s.last_activity_at AS last_activity_at
        FROM history_fts JOIN history_documents d ON d.id=history_fts.rowid JOIN history_sources s ON s.path=d.source_path
		WHERE ` + strings.Join(where, ` AND `)
	query := baseQuery + ` ORDER BY bm25(history_fts),d.source_path,d.message_index,d.part_index,d.id LIMIT ?`
	if after := req.After; after != nil {
		query = `WITH ranked AS MATERIALIZED (` + baseQuery + `)
			SELECT * FROM ranked WHERE rank>? OR (rank=? AND source_path>?) OR
			(rank=? AND source_path=? AND message_index>?) OR
			(rank=? AND source_path=? AND message_index=? AND part_index>?) OR
			(rank=? AND source_path=? AND message_index=? AND part_index=? AND id>?)
			ORDER BY rank,source_path,message_index,part_index,id LIMIT ?`
		args = append(args, after.Rank, after.Rank, after.SessionPath,
			after.Rank, after.SessionPath, after.MessageIndex,
			after.Rank, after.SessionPath, after.MessageIndex, after.PartIndex,
			after.Rank, after.SessionPath, after.MessageIndex, after.PartIndex, after.RowID)
	}
	args = append(args, limit)
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Candidate
		if err := rows.Scan(&item.RowID, &item.SessionPath, &item.Root, &item.Source, &item.Scope, &item.WorkspaceRoot, &item.ContentDigest,
			&item.MessageIndex, &item.PartIndex, &item.Role, &item.Kind, &item.ToolName, &item.Rank,
			&item.SessionTitle, &item.TopicTitle, &item.LastActivityAt); err != nil {
			return out, err
		}
		if !catalogPathWithin(item.SessionPath, item.Root) {
			continue
		}
		if item.Rank < 0 {
			item.Score = -item.Rank
		} else {
			item.Score = 1 / (1 + item.Rank)
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

func catalogPathWithin(path, root string) bool {
	absPath, err := filepath.Abs(filepath.Clean(strings.TrimSpace(path)))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(strings.TrimSpace(root)))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func (c *Catalog) Purge(ctx context.Context, path string) error {
	if c == nil {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_fts WHERE rowid IN (SELECT id FROM history_documents WHERE source_path=?)`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_sources WHERE path=?`, path); err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = c.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	_, _ = c.db.ExecContext(ctx, `PRAGMA incremental_vacuum(64)`)
	c.publish(revision, nil, "purge")
	return nil
}

func (c *Catalog) refreshStatus(ctx context.Context) {
	var indexed, total, pending, failed int64
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_sources WHERE health='ok'`).Scan(&indexed)
	_ = c.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM history_roots`).Scan(&total)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_roots WHERE state<>'ready'`).Scan(&pending)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_sources WHERE health='corrupt'`).Scan(&failed)
	c.statusMu.Lock()
	c.status.Indexed, c.status.Total, c.status.Pending, c.status.Failed = indexed, total, pending, failed
	c.status.Revision = c.revision.Load()
	c.statusMu.Unlock()
}

func (c *Catalog) publish(revision uint64, roots []string, reason string) {
	c.revision.Store(revision)
	c.refreshStatus(context.Background())
	if c.opts.OnRevision != nil {
		c.opts.OnRevision(c.Status(), roots, reason)
	}
}

func (c *Catalog) setError(err error) {
	c.statusMu.Lock()
	c.status.LastError = err.Error()
	c.status.Failed++
	c.statusMu.Unlock()
}

func (c *Catalog) Status() Status {
	if c == nil {
		return Status{State: "degraded", Mode: projectiondb.ModeMemory, LastError: "history catalog unavailable"}
	}
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}

func (c *Catalog) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.cancel()
		go func() {
			c.wg.Wait()
			c.closeErr = c.db.Close()
			close(c.closeDone)
		}()
	})
	select {
	case <-c.closeDone:
		return c.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
