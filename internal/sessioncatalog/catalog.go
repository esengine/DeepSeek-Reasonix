package sessioncatalog

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/projectiondb"
)

const defaultMissingGrace = 30 * time.Second

type Catalog struct {
	db          *sql.DB
	opts        Options
	revision    atomic.Uint64
	statusMu    sync.RWMutex
	status      Status
	writeCh     chan string
	writeMu     sync.Mutex
	writeQueued map[string]SessionRecord
	// mutationMu is the process-local SQLite single-writer boundary. WAL permits
	// concurrent readers, but repair, metadata, and reconcile mutations must not
	// race into avoidable SQLITE_BUSY failures.
	mutationMu       sync.Mutex
	removedPaths     sync.Map
	repairCh         chan string
	repairQueued     sync.Map
	reconcileCh      chan DirectoryTarget
	reconcileQueued  sync.Map
	reconcileDirtyMu sync.Mutex
	reconcileDirty   map[string]DirectoryTarget
	pathCh           chan sessionPathRequest
	pathQueued       sync.Map
	directoryLocksMu sync.Mutex
	directoryLocks   map[string]*sync.Mutex
	workerCtx        context.Context
	workerCancel     context.CancelFunc
	stop             chan struct{}
	stopOnce         sync.Once
	workers          sync.WaitGroup
	closeDone        chan struct{}
	closeErr         error
}

type sessionPathRequest struct {
	target DirectoryTarget
	path   string
}

type pageCursor struct {
	Pinned      int    `json:"p"`
	ManualOrder bool   `json:"m,omitempty"`
	SortOrder   int64  `json:"o,omitempty"`
	Activity    int64  `json:"a"`
	TopicID     string `json:"t"`
}

func Open(ctx context.Context, opts Options) (*Catalog, error) {
	if opts.Path == "" {
		opts.Path = DefaultPath()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MissingGrace <= 0 {
		opts.MissingGrace = defaultMissingGrace
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 1024
	}
	// An empty path (no cache dir) or explicit memory flag must never write a
	// relative session-catalog file into the current project directory.
	if strings.TrimSpace(opts.Path) == "" {
		opts.Path = ""
		opts.InMemory = true
	}
	if !opts.InMemory {
		if env := strings.TrimSpace(os.Getenv("REASONIX_SESSION_CATALOG_MEMORY")); env == "1" {
			opts.InMemory = true
		}
	}

	c := &Catalog{
		opts:           opts,
		writeCh:        make(chan string, opts.QueueCapacity),
		writeQueued:    map[string]SessionRecord{},
		repairCh:       make(chan string, opts.QueueCapacity),
		reconcileCh:    make(chan DirectoryTarget, 64),
		reconcileDirty: map[string]DirectoryTarget{},
		pathCh:         make(chan sessionPathRequest, opts.QueueCapacity),
		directoryLocks: map[string]*sync.Mutex{},
		stop:           make(chan struct{}),
		closeDone:      make(chan struct{}),
		status:         Status{State: StateOpening, Path: opts.Path},
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path:         opts.Path,
		MemoryName:   "session-catalog",
		Migrations:   sessionMigrations(),
		InMemory:     opts.InMemory,
		MaxOpenConns: 4,
		Now:          opts.Now,
	})
	if err != nil {
		return nil, err
	}
	c.db = handle.DB
	c.status.Mode = Mode(handle.Status.Mode)
	c.status.State = State(handle.Status.State)
	if c.status.State == "" {
		c.status.State = StateReady
	}
	if c.status.Mode == ModeMemory {
		c.status.Path = ""
	} else {
		c.status.Path = handle.Status.Path
	}
	c.status.LastError = handle.Status.LastError
	c.status.QuarantinedPath = handle.Status.QuarantinedPath
	if err := c.loadStatus(ctx); err != nil {
		_ = c.db.Close()
		return nil, err
	}
	c.workerCtx, c.workerCancel = context.WithCancel(context.Background())
	c.workers.Add(1)
	go c.writerLoop()
	c.workers.Add(1)
	go c.reconcileLoop()
	c.workers.Add(1)
	go c.sessionPathLoop()
	if !opts.DisableRepair {
		c.workers.Add(1)
		go c.repairLoop()
		c.enqueuePersistedRepairs(ctx)
	}
	return c, nil
}

func (c *Catalog) loadStatus(ctx context.Context) error {
	var revision uint64
	if err := c.db.QueryRowContext(ctx, `SELECT revision FROM catalog_state WHERE id=1`).Scan(&revision); err != nil {
		return err
	}
	c.revision.Store(revision)
	c.statusMu.Lock()
	c.status.Revision = revision
	c.statusMu.Unlock()
	c.refreshCounts(ctx)
	return nil
}

func (c *Catalog) Status() Status {
	if c == nil {
		return Status{State: StateDegraded, Mode: ModeMemory, LastError: "session catalog unavailable"}
	}
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}

func (c *Catalog) refreshCounts(ctx context.Context) {
	if c == nil || c.db == nil {
		return
	}
	var indexed, pending, total, physical, logical, groups, branches, diverged, cleanup int64
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions`).Scan(&indexed)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE turns_state='unknown'`).Scan(&pending)
	_ = c.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total), 0) FROM catalog_directories`).Scan(&total)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE missing_since=0`).Scan(&physical)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_topics`).Scan(&logical)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT recovery_group_id) FROM catalog_sessions WHERE recovered=1 AND recovery_group_id<>'' AND missing_since=0`).Scan(&groups)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND missing_since=0`).Scan(&branches)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND recovery_role='diverged' AND missing_since=0`).Scan(&diverged)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE recovered=1 AND recovery_role='covered_copy' AND missing_since=0`).Scan(&cleanup)
	c.statusMu.Lock()
	c.status.Indexed = indexed
	c.status.Total = total
	c.status.RepairPending = pending
	c.status.PhysicalSessions = physical
	c.status.LogicalSessions = logical
	c.status.RecoveryGroups = groups
	c.status.RecoveryBranches = branches
	c.status.RecoveryDiverged = diverged
	c.status.CleanupEligible = cleanup
	c.status.SourceCount = total
	c.status.Revision = c.revision.Load()
	c.statusMu.Unlock()
}

func (c *Catalog) markRepair(reason string, at int64) {
	if c == nil || strings.TrimSpace(reason) == "" {
		return
	}
	if at <= 0 {
		at = time.Now().UnixMilli()
	}
	c.statusMu.Lock()
	c.status.RepairReason = strings.TrimSpace(reason)
	c.status.LastRepairAt = at
	c.statusMu.Unlock()
}

// MarkRepairReason records a lifecycle-level repair cause (for example, a
// clean index-generation cutover) without touching the authoritative session
// files. Integrity checks use the internal helper so they can attach their
// timestamp at the point of detection.
func (c *Catalog) MarkRepairReason(reason string) {
	if c == nil {
		return
	}
	c.markRepair(reason, c.opts.Now().UnixMilli())
}

func normalizeScope(scope, root string) (string, string) {
	if strings.TrimSpace(scope) != "project" {
		return "global", ""
	}
	return "project", strings.TrimSpace(root)
}

func normalizeSessionRecord(record SessionRecord) SessionRecord {
	record.Path = filepath.Clean(record.Path)
	if record.Directory == "" {
		record.Directory = filepath.Dir(record.Path)
	}
	record.Directory = filepath.Clean(record.Directory)
	record.Scope, record.WorkspaceRoot = normalizeScope(record.Scope, record.WorkspaceRoot)
	if record.TurnsState == "" {
		record.TurnsState = TurnsUnknown
	}
	if record.Health == "" {
		record.Health = HealthOK
	}
	return record
}

func (c *Catalog) EnqueueSession(record SessionRecord) bool {
	if c == nil {
		return false
	}
	record = normalizeSessionRecord(record)
	c.removedPaths.Delete(record.Path)
	c.writeMu.Lock()
	if _, loaded := c.writeQueued[record.Path]; loaded {
		c.writeQueued[record.Path] = record
		c.writeMu.Unlock()
		return true
	}
	c.writeQueued[record.Path] = record
	select {
	case <-c.stop:
		delete(c.writeQueued, record.Path)
		c.writeMu.Unlock()
		return false
	case c.writeCh <- record.Path:
		c.writeMu.Unlock()
		return true
	default:
		delete(c.writeQueued, record.Path)
		c.writeMu.Unlock()
		return false
	}
}

func (c *Catalog) takeQueuedWrite(path string) (SessionRecord, bool) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	record, ok := c.writeQueued[path]
	if ok {
		delete(c.writeQueued, path)
	}
	return record, ok
}

func (c *Catalog) writerLoop() {
	defer c.workers.Done()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	pending := map[string]SessionRecord{}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		records := make([]SessionRecord, 0, len(pending))
		for _, record := range pending {
			records = append(records, record)
		}
		pending = map[string]SessionRecord{}
		ctx, cancel := context.WithTimeout(c.workerCtx, time.Second)
		_ = c.upsertSessions(ctx, records, nil, "write")
		cancel()
	}
	for {
		select {
		case path := <-c.writeCh:
			if record, ok := c.takeQueuedWrite(path); ok {
				pending[path] = record
			}
			if len(pending) >= 64 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-c.stop:
			for {
				select {
				case path := <-c.writeCh:
					if record, ok := c.takeQueuedWrite(path); ok {
						pending[path] = record
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

func (c *Catalog) UpsertSession(ctx context.Context, record SessionRecord) error {
	return c.upsertSessions(ctx, []SessionRecord{normalizeSessionRecord(record)}, nil, "write")
}

func (c *Catalog) upsertSessions(ctx context.Context, records []SessionRecord, generations map[string]int64, reason string) error {
	return c.upsertSessionsWithNotification(ctx, records, generations, reason, true)
}

func (c *Catalog) upsertSessionsWithoutNotification(ctx context.Context, records []SessionRecord, generations map[string]int64, reason string) error {
	return c.upsertSessionsWithNotification(ctx, records, generations, reason, false)
}

func (c *Catalog) upsertSessionsWithNotification(ctx context.Context, records []SessionRecord, generations map[string]int64, reason string, notify bool) error {
	if len(records) == 0 {
		return nil
	}
	c.mutationMu.Lock()
	defer c.mutationMu.Unlock()
	filtered := records[:0]
	for _, record := range records {
		if _, removed := c.removedPaths.Load(filepath.Clean(record.Path)); !removed {
			filtered = append(filtered, record)
		}
	}
	records = filtered
	if len(records) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	affected := map[TopicKey]struct{}{}
	roots := map[string]struct{}{}
	directoryGenerations := map[string]int64{}
	for _, raw := range records {
		record := normalizeSessionRecord(raw)
		var previous TopicKey
		if err := tx.QueryRowContext(ctx, `SELECT scope,workspace_root,topic_id FROM catalog_sessions WHERE path=?`, record.Path).
			Scan(&previous.Scope, &previous.WorkspaceRoot, &previous.TopicID); err == nil && previous.TopicID != "" {
			affected[previous] = struct{}{}
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return err
		}
		generation := int64(0)
		if generations != nil {
			generation = generations[record.Path]
		} else if cached, ok := directoryGenerations[record.Directory]; ok {
			generation = cached
		} else {
			_ = tx.QueryRowContext(ctx, `SELECT scan_generation FROM catalog_directories WHERE path=?`, record.Directory).Scan(&generation)
			directoryGenerations[record.Directory] = generation
		}
		record = classifyRecoveryLineage(record)
		if record.LogicalTopicID == "" {
			record.LogicalTopicID = record.TopicID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_sessions(
            path,directory,scope,workspace_root,topic_id,topic_title,custom_title,
            created_at,last_activity_at,preview,turns,turns_state,recovered,
            recovery_reason,recovery_digest,parent_id,recovery_copy,recovery_group_id,
            recovery_role,recovery_canonical,logical_topic_id,ordinary_visible,content_fingerprint,
            meta_fingerprint,health,missing_since,seen_generation
        ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(path) DO UPDATE SET
            directory=excluded.directory, scope=excluded.scope,
            workspace_root=excluded.workspace_root, topic_id=excluded.topic_id,
            topic_title=excluded.topic_title, custom_title=excluded.custom_title,
            created_at=excluded.created_at, last_activity_at=excluded.last_activity_at,
            preview=excluded.preview, turns=excluded.turns,
            turns_state=excluded.turns_state, recovered=excluded.recovered,
            recovery_reason=excluded.recovery_reason,
            recovery_digest=excluded.recovery_digest, parent_id=excluded.parent_id,
            recovery_copy=excluded.recovery_copy,
            recovery_group_id=excluded.recovery_group_id,
            recovery_role=excluded.recovery_role,
            recovery_canonical=excluded.recovery_canonical,
            logical_topic_id=excluded.logical_topic_id,
            ordinary_visible=excluded.ordinary_visible,
            content_fingerprint=excluded.content_fingerprint,
            meta_fingerprint=excluded.meta_fingerprint, health=excluded.health,
            missing_since=0, seen_generation=MAX(catalog_sessions.seen_generation, excluded.seen_generation)`,
			record.Path, record.Directory, record.Scope, record.WorkspaceRoot,
			record.TopicID, record.TopicTitle, record.CustomTitle, record.CreatedAt,
			record.LastActivityAt, record.Preview, record.Turns, record.TurnsState,
			record.Recovered, record.RecoveryReason, record.RecoveryDigest,
			record.ParentID, boolToInt(record.RecoveryCopy), record.RecoveryGroupID,
			record.RecoveryRole, boolToInt(record.RecoveryCanonical),
			record.LogicalTopicID, boolToInt(record.OrdinaryVisible),
			record.ContentFingerprint, record.MetaFingerprint,
			record.Health, 0, generation); err != nil {
			_ = tx.Rollback()
			return err
		}
		if record.TopicID != "" {
			affected[TopicKey{Scope: record.Scope, WorkspaceRoot: record.WorkspaceRoot, TopicID: record.TopicID}] = struct{}{}
		}
		if err := updateFoldedTopicTombstones(ctx, tx, previous, record, c.opts.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		roots[record.WorkspaceRoot] = struct{}{}
	}
	for key := range affected {
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
	if notify {
		c.publishRevision(revision, mapKeys(roots), reason)
	} else {
		c.rememberRevision(revision)
	}
	c.refreshCounts(ctx)
	return nil
}

func recomputeTopic(ctx context.Context, tx *sql.Tx, key TopicKey) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?`, key.Scope, key.WorkspaceRoot, key.TopicID).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM catalog_topics WHERE scope=? AND workspace_root=? AND topic_id=?`, key.Scope, key.WorkspaceRoot, key.TopicID)
		return err
	}
	// Covered copies skip turn/health totals but still update recency. Adopted
	// branches are alternate continuations, so preserve the pre-catalog contract:
	// max(sum(normal turns), max(adopted recovery turns)).
	_, err := tx.ExecContext(ctx, `INSERT INTO catalog_topics(
        scope,workspace_root,topic_id,title,turns,turns_state,created_at,
        last_activity_at,recovery_state,recovery_branch_count,
        recovery_unresolved_count,recovery_cleanup_eligible_count,health
    ) SELECT ?,?,?,
        COALESCE(NULLIF((SELECT COALESCE(NULLIF(custom_title,''), NULLIF(topic_title,''), preview, '')
            FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?
            ORDER BY recovery_copy ASC, last_activity_at DESC, path ASC LIMIT 1),''), ?),
		MAX(
			COALESCE(SUM(CASE WHEN recovery_copy=0 AND recovered=0 AND turns_state='valid' THEN turns ELSE 0 END),0),
			COALESCE(MAX(CASE WHEN recovery_copy=0 AND recovered=1 AND turns_state='valid' THEN turns ELSE 0 END),0)
		),
        CASE WHEN SUM(CASE WHEN recovery_copy=0 AND turns_state='corrupt' THEN 1 ELSE 0 END)>0 THEN 'corrupt'
             WHEN SUM(CASE WHEN recovery_copy=0 AND turns_state='unknown' THEN 1 ELSE 0 END)>0 THEN 'unknown'
             WHEN SUM(CASE WHEN recovery_copy=0 THEN 1 ELSE 0 END)=0 THEN 'valid'
             ELSE 'valid' END,
        COALESCE(MIN(NULLIF(created_at,0)),0), COALESCE(MAX(last_activity_at),0),
        CASE WHEN SUM(CASE WHEN recovered=1 AND recovery_role='preferred' THEN 1 ELSE 0 END)>0 THEN 'preferred'
             WHEN SUM(CASE WHEN recovered=1 AND recovery_role='diverged' THEN 1 ELSE 0 END)>0 THEN 'diverged'
             WHEN SUM(CASE WHEN recovered=1 AND recovery_role='adopted' THEN 1 ELSE 0 END)>0 THEN 'adopted'
             WHEN SUM(CASE WHEN recovery_copy=0 THEN 1 ELSE 0 END)=0 THEN 'recovery_only' ELSE '' END,
        SUM(CASE WHEN recovered=1 THEN 1 ELSE 0 END),
        CASE WHEN SUM(CASE WHEN recovered=1 AND recovery_role='preferred' THEN 1 ELSE 0 END)>0 THEN 0
             ELSE SUM(CASE WHEN recovered=1 AND recovery_role='diverged' THEN 1 ELSE 0 END) END,
        SUM(CASE WHEN recovered=1 AND recovery_role='covered_copy' THEN 1 ELSE 0 END),
        CASE WHEN SUM(CASE WHEN recovery_copy=0 AND health='corrupt' THEN 1 ELSE 0 END)>0 THEN 'corrupt'
             WHEN SUM(CASE WHEN recovery_copy=0 AND health='missing' THEN 1 ELSE 0 END)>0 THEN 'missing'
             ELSE 'ok' END
      FROM catalog_sessions WHERE scope=? AND workspace_root=? AND topic_id=?
    ON CONFLICT(scope,workspace_root,topic_id) DO UPDATE SET
        title=excluded.title, turns=excluded.turns, turns_state=excluded.turns_state,
        created_at=excluded.created_at, last_activity_at=excluded.last_activity_at,
        recovery_state=excluded.recovery_state,
        recovery_branch_count=excluded.recovery_branch_count,
        recovery_unresolved_count=excluded.recovery_unresolved_count,
        recovery_cleanup_eligible_count=excluded.recovery_cleanup_eligible_count,
        health=excluded.health`,
		key.Scope, key.WorkspaceRoot, key.TopicID,
		key.Scope, key.WorkspaceRoot, key.TopicID, key.TopicID,
		key.Scope, key.WorkspaceRoot, key.TopicID)
	return err
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func bumpRevision(ctx context.Context, tx *sql.Tx) (uint64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_state SET revision=revision+1 WHERE id=1`); err != nil {
		return 0, err
	}
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM catalog_state WHERE id=1`).Scan(&revision); err != nil {
		return 0, err
	}
	return revision, nil
}

func (c *Catalog) publishRevision(revision uint64, roots []string, reason string) {
	c.rememberRevision(revision)
	if c.opts.OnRevision != nil {
		c.opts.OnRevision(revision, roots, reason)
	}
}

func (c *Catalog) rememberRevision(revision uint64) {
	c.revision.Store(revision)
	c.statusMu.Lock()
	c.status.Revision = revision
	c.statusMu.Unlock()
}

func mapKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}
func (c *Catalog) listTopicSessions(ctx context.Context, key TopicKey) ([]SessionRecord, error) {
	out := []SessionRecord{}
	var cursor *sessionPageCursor
	for len(out) < MaxLimit {
		where := `scope=? AND workspace_root=? AND topic_id=?`
		args := []any{key.Scope, key.WorkspaceRoot, key.TopicID}
		if cursor != nil {
			where += ` AND (last_activity_at<? OR (last_activity_at=? AND path>?))`
			args = append(args, cursor.Activity, cursor.Activity, cursor.Path)
		}
		args = append(args, MaxLimit)
		rows, err := c.db.QueryContext(ctx, `SELECT `+sessionSelectColumns+` FROM catalog_sessions
            WHERE `+where+` ORDER BY last_activity_at DESC,path ASC LIMIT ?`, args...)
		if err != nil {
			return nil, err
		}
		rawCount := 0
		var lastScanned SessionRecord
		for rows.Next() {
			record, err := scanSession(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			rawCount++
			lastScanned = record
			if c.pathRemoved(record.Path) {
				continue
			}
			out = append(out, record)
			if len(out) == MaxLimit {
				break
			}
		}
		rowsErr := rows.Err()
		_ = rows.Close()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if len(out) == MaxLimit || rawCount < MaxLimit || rawCount == 0 {
			break
		}
		cursor = &sessionPageCursor{Activity: lastScanned.LastActivityAt, Path: lastScanned.Path}
	}
	return out, nil
}

func (c *Catalog) GetTopic(ctx context.Context, key TopicKey) (TopicRecord, bool, error) {
	key.Scope, key.WorkspaceRoot = normalizeScope(key.Scope, key.WorkspaceRoot)
	key.TopicID = strings.TrimSpace(key.TopicID)
	item := TopicRecord{Sessions: []SessionRecord{}}
	err := c.db.QueryRowContext(ctx, `SELECT scope,workspace_root,topic_id,title,title_source,pinned,
		CASE WHEN metadata_present=1 THEN sort_order ELSE -1 END,
        turns,turns_state,created_at,last_activity_at,recovery_state,recovery_branch_count,
        recovery_unresolved_count,recovery_cleanup_eligible_count,health
        FROM catalog_topics WHERE scope=? AND workspace_root=? AND topic_id=?`,
		key.Scope, key.WorkspaceRoot, key.TopicID).Scan(
		&item.Scope, &item.WorkspaceRoot, &item.TopicID, &item.Title, &item.TitleSource,
		&item.Pinned, &item.SortOrder, &item.Turns, &item.TurnsState,
		&item.CreatedAt, &item.LastActivityAt, &item.RecoveryState, &item.RecoveryBranchCount,
		&item.RecoveryUnresolvedCount, &item.RecoveryCleanupEligibleCount, &item.Health)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if err != nil {
		return item, false, err
	}
	item.Sessions, err = c.listTopicSessions(ctx, key)
	if err != nil {
		return TopicRecord{Sessions: []SessionRecord{}}, false, err
	}
	// Tombstone overlay: topic rows may lag behind RemoveSession while the
	// durable DELETE waits on locks or a short caller context.
	if len(item.Sessions) == 0 {
		return TopicRecord{Sessions: []SessionRecord{}}, false, nil
	}
	hydrateTopicDisplay(&item)
	return item, true, nil
}

func topicRepresentativePath(sessions []SessionRecord) string {
	if path := OrdinaryContinuePath(sessions, ""); path != "" {
		return path
	}
	preferred := PreferredOrdinarySessionPaths(sessions)
	best := SessionRecord{}
	found := false
	for _, session := range sessions {
		path := strings.TrimSpace(session.Path)
		_, isPreferred := preferred[path]
		if !session.OrdinaryVisible && !isPreferred && (session.Recovered || session.RecoveryCopy) {
			continue
		}
		if !found || recoveryRank(session) > recoveryRank(best) ||
			(recoveryRank(session) == recoveryRank(best) && session.LastActivityAt > best.LastActivityAt) {
			best = session
			found = true
		}
	}
	if found {
		return best.Path
	}
	if len(sessions) > 0 {
		return sessions[0].Path
	}
	return ""
}

// EncodeTopicCursor builds an exclusive ListTopics keyset cursor after the
// given topic position. Desktop post-filters recovery-only rows and needs the
// same cursor shape catalog.ListTopics emits.
func EncodeTopicCursor(pinned int, lastActivityAt int64, topicID string) string {
	return encodeCursor(pageCursor{Pinned: pinned, Activity: lastActivityAt, TopicID: topicID})
}

// EncodeOrderedTopicCursor builds a cursor for a workspace with explicit
// manual topic ordering. A negative sortOrder places metadata-free/runtime
// topics after every explicitly ranked topic in the same pinned bucket.
func EncodeOrderedTopicCursor(pinned, sortOrder int, lastActivityAt int64, topicID string) string {
	manualSortOrder := int64(sortOrder)
	if sortOrder < 0 {
		manualSortOrder = unrankedTopicSortOrder
	}
	return encodeCursor(pageCursor{
		Pinned: pinned, ManualOrder: true, SortOrder: manualSortOrder,
		Activity: lastActivityAt, TopicID: topicID,
	})
}

func encodeCursor(cursor pageCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(encoded string) (*pageCursor, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid session catalog cursor: %w", err)
	}
	var cursor pageCursor
	if err := json.Unmarshal(b, &cursor); err != nil || cursor.TopicID == "" {
		return nil, errors.New("invalid session catalog cursor")
	}
	return &cursor, nil
}

func timeFilterCutoff(filter string, now time.Time) int64 {
	var duration time.Duration
	value := strings.TrimSpace(strings.ToLower(filter))
	switch value {
	case "day", "24h":
		duration = 24 * time.Hour
	case "week", "7d":
		duration = 7 * 24 * time.Hour
	case "month", "30d":
		duration = 30 * 24 * time.Hour
	default:
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return 0
		}
		duration = parsed
	}
	return now.Add(-duration).UnixMilli()
}

func (c *Catalog) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.stopOnce.Do(func() {
		if c.workerCancel != nil {
			c.workerCancel()
		}
		close(c.stop)
		go func() {
			c.workers.Wait()
			c.closeErr = c.db.Close()
			c.statusMu.Lock()
			c.status.State = StateClosed
			c.statusMu.Unlock()
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
