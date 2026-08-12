// Package taskcatalog maintains a disposable cross-project projection of the
// authoritative taskmonitor FileStore.
package taskcatalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/projectiondb"
	"reasonix/internal/taskmonitor"
)

const (
	SchemaVersion = 1
	DefaultLimit  = 50
	MaxLimit      = 200
	missingGrace  = 30 * time.Second
)

type Status struct {
	State     string            `json:"state"`
	Mode      projectiondb.Mode `json:"mode"`
	Path      string            `json:"path,omitempty"`
	Revision  uint64            `json:"revision"`
	Indexed   int64             `json:"indexed"`
	Total     int64             `json:"total"`
	Pending   int64             `json:"pending"`
	Failed    int64             `json:"failed"`
	LastError string            `json:"lastError,omitempty"`
}

type Project struct {
	Key   string `json:"projectKey"`
	Root  string `json:"projectRoot"`
	Label string `json:"projectLabel"`
}

type PageRequest struct {
	ProjectKeys []string
	SessionID   string
	States      []string
	Query       string
	Cursor      string
	Limit       int
}

type Item struct {
	ProjectKey   string                   `json:"projectKey"`
	ProjectLabel string                   `json:"projectLabel"`
	Task         taskmonitor.TaskSnapshot `json:"task"`
}

type Page struct {
	Items       []Item `json:"items"`
	NextCursor  string `json:"nextCursor"`
	Revision    uint64 `json:"revision"`
	Partial     bool   `json:"partial"`
	StaleCursor bool   `json:"staleCursor"`
	Status      Status `json:"status"`
}

type EventPage struct {
	Items        []taskmonitor.TaskEvent `json:"items"`
	NextSequence int                     `json:"nextSequence"`
	Partial      bool                    `json:"partial"`
}

type cursor struct {
	Revision uint64 `json:"r"`
	Updated  int64  `json:"u"`
	Project  string `json:"p"`
	Task     string `json:"t"`
}

type request struct {
	projectRoot string
	taskID      string
	events      bool
	flush       chan struct{}
}

type Catalog struct {
	db            *sql.DB
	store         *taskmonitor.FileStore
	ctx           context.Context
	cancel        context.CancelFunc
	queue         chan request
	dirtyWake     chan struct{}
	dirtyProjects sync.Map
	wg            sync.WaitGroup
	closing       atomic.Bool
	revision      atomic.Uint64
	statusMu      sync.RWMutex
	status        Status
	projectLocks  sync.Map
	reconcileMu   sync.Mutex
	reconciling   map[string]bool
	registered    map[string]bool
	reconcileDone bool
	closeOnce     sync.Once
	closeDone     chan struct{}
	closeErr      error
}

// DefaultPath returns the disposable task projection path under CacheDir.
// Empty when cache is unavailable so Open falls back to an in-memory projection.
func DefaultPath() string {
	cache := strings.TrimSpace(config.CacheDir())
	if cache == "" {
		return ""
	}
	return filepath.Join(cache, "task-catalog", "v1.sqlite")
}

func ProjectKey(root string) string {
	root = filepath.Clean(strings.TrimSpace(root))
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])
}

const schema = `
CREATE TABLE task_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL DEFAULT 0);
INSERT INTO task_state(id,revision) VALUES(1,0);
CREATE TABLE task_projects(project_key TEXT PRIMARY KEY,project_root TEXT UNIQUE NOT NULL,project_label TEXT NOT NULL DEFAULT '',
 signature TEXT NOT NULL DEFAULT '',scan_generation INTEGER NOT NULL DEFAULT 0,scan_cursor TEXT NOT NULL DEFAULT '',state TEXT NOT NULL DEFAULT 'pending',
 error TEXT NOT NULL DEFAULT '',indexed INTEGER NOT NULL DEFAULT 0,total INTEGER NOT NULL DEFAULT 0,completed_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE task_snapshots(project_key TEXT NOT NULL,task_id TEXT NOT NULL,session_id TEXT NOT NULL DEFAULT '',job_id TEXT NOT NULL DEFAULT '',
 kind TEXT NOT NULL DEFAULT '',label TEXT NOT NULL DEFAULT '',state TEXT NOT NULL,runtime_state TEXT NOT NULL DEFAULT '',runtime_lease_until INTEGER NOT NULL DEFAULT 0,
 version INTEGER NOT NULL,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL,error_code TEXT NOT NULL DEFAULT '',snapshot_fingerprint TEXT NOT NULL DEFAULT '',
 snapshot_json BLOB NOT NULL,health TEXT NOT NULL DEFAULT 'ok',missing_since INTEGER NOT NULL DEFAULT 0,seen_generation INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(project_key,task_id),FOREIGN KEY(project_key) REFERENCES task_projects(project_key) ON DELETE CASCADE);
CREATE TABLE task_event_sources(project_key TEXT NOT NULL,task_id TEXT NOT NULL,path TEXT NOT NULL,size INTEGER NOT NULL DEFAULT 0,indexed_offset INTEGER NOT NULL DEFAULT 0,
 fingerprint TEXT NOT NULL DEFAULT '',state TEXT NOT NULL DEFAULT 'pending',error TEXT NOT NULL DEFAULT '',PRIMARY KEY(project_key,task_id));
CREATE TABLE task_events(project_key TEXT NOT NULL,task_id TEXT NOT NULL,sequence INTEGER NOT NULL,timestamp INTEGER NOT NULL,event_type TEXT NOT NULL,
 state TEXT NOT NULL,runtime_state TEXT NOT NULL DEFAULT '',event_json BLOB NOT NULL,PRIMARY KEY(project_key,task_id,sequence));
CREATE INDEX idx_task_project_page ON task_snapshots(project_key,updated_at DESC,task_id);
CREATE INDEX idx_task_state_page ON task_snapshots(state,updated_at DESC,project_key,task_id);
CREATE INDEX idx_task_session ON task_snapshots(session_id,updated_at DESC,task_id);
CREATE INDEX idx_task_events_page ON task_events(project_key,task_id,sequence);
`

func migrations() []projectiondb.Migration {
	return []projectiondb.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, schema)
		return err
	}}}
}

func Open(ctx context.Context, path string) (*Catalog, error) {
	if path == "" {
		path = DefaultPath()
	}
	inMemory := strings.TrimSpace(path) == ""
	if inMemory {
		path = ""
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{
		Path: path, MemoryName: "task-catalog", Migrations: migrations(), InMemory: inMemory, MaxOpenConns: 4,
	})
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	c := &Catalog{db: handle.DB, store: taskmonitor.NewFileStore(filepath.Join(".reasonix", "tasks")), ctx: workerCtx, cancel: cancel,
		queue: make(chan request, 1024), dirtyWake: make(chan struct{}, 1), reconciling: map[string]bool{}, registered: map[string]bool{}, closeDone: make(chan struct{}),
		status: Status{State: string(handle.Status.State), Mode: handle.Status.Mode, Path: handle.Status.Path, LastError: handle.Status.LastError}}
	var revision uint64
	_ = c.db.QueryRowContext(ctx, `SELECT revision FROM task_state WHERE id=1`).Scan(&revision)
	c.revision.Store(revision)
	c.refresh(ctx)
	c.wg.Add(1)
	go c.worker()
	return c, nil
}

func (c *Catalog) ObservedStore() *taskmonitor.FileStore {
	return taskmonitor.NewObservedFileStore(filepath.Join(".reasonix", "tasks"), c)
}

func (c *Catalog) SnapshotChanged(projectRoot, taskID string) {
	c.enqueue(request{projectRoot: projectRoot, taskID: taskID})
}
func (c *Catalog) EventsChanged(projectRoot, taskID string) {
	c.enqueue(request{projectRoot: projectRoot, taskID: taskID, events: true})
}

func (c *Catalog) enqueue(req request) {
	if c.closing.Load() {
		return
	}
	select {
	case c.queue <- req:
	default:
		c.dirtyProjects.Store(req.projectRoot, true)
		c.wakeDirty()
	}
}

func (c *Catalog) worker() {
	defer c.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if root, ok := c.takeDirtyProject(); ok {
			if project, exists, err := c.projectByRoot(c.ctx, root); err == nil && exists {
				_ = c.ReconcileProject(c.ctx, project)
			} else if err == nil {
				_, _ = c.RegisterProject(c.ctx, root, filepath.Base(root))
			}
			continue
		}
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.markRegisteredProjectsDirty()
		case <-c.dirtyWake:
		case req := <-c.queue:
			if req.flush != nil {
				close(req.flush)
				continue
			}
			if req.events {
				_ = c.indexEvents(c.ctx, req.projectRoot, req.taskID)
			} else {
				_ = c.indexSnapshot(c.ctx, req.projectRoot, req.taskID, 0)
			}
		}
	}
}

func (c *Catalog) takeDirtyProject() (string, bool) {
	var root string
	c.dirtyProjects.Range(func(key, _ any) bool {
		root, _ = key.(string)
		c.dirtyProjects.Delete(key)
		return false
	})
	return root, root != ""
}

func (c *Catalog) markRegisteredProjectsDirty() {
	rows, err := c.db.QueryContext(c.ctx, `SELECT project_root FROM task_projects`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var root string
		if rows.Scan(&root) == nil {
			c.dirtyProjects.Store(root, true)
		}
	}
}

func (c *Catalog) wakeDirty() {
	select {
	case c.dirtyWake <- struct{}{}:
	default:
	}
}

func (c *Catalog) Flush(ctx context.Context) error {
	done := make(chan struct{})
	select {
	case c.queue <- request{flush: done}:
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

func (c *Catalog) RegisterProject(ctx context.Context, root, label string) (Project, error) {
	project := normalizeProject(Project{Root: root, Label: label})
	_, err := c.db.ExecContext(ctx, `INSERT INTO task_projects(project_key,project_root,project_label,state) VALUES(?,?,?,'pending')
        ON CONFLICT(project_key) DO UPDATE SET project_root=excluded.project_root,project_label=excluded.project_label`, project.Key, project.Root, project.Label)
	c.reconcileMu.Lock()
	firstRegistration := !c.registered[project.Key]
	c.registered[project.Key] = true
	c.reconcileMu.Unlock()
	if err == nil && firstRegistration {
		c.scheduleReconcile(project)
	}
	return project, err
}

// RequestReconcileProject forces a background authority scan even when the
// project was already registered by an earlier page request.
func (c *Catalog) RequestReconcileProject(ctx context.Context, root, label string) error {
	project, err := c.RegisterProject(ctx, root, label)
	if err != nil {
		return err
	}
	c.scheduleReconcile(project)
	return nil
}

func (c *Catalog) scheduleReconcile(project Project) {
	c.reconcileMu.Lock()
	if c.reconcileDone || c.reconciling[project.Key] {
		c.reconcileMu.Unlock()
		return
	}
	c.reconciling[project.Key] = true
	c.wg.Add(1)
	c.reconcileMu.Unlock()
	go func() {
		defer func() {
			c.reconcileMu.Lock()
			delete(c.reconciling, project.Key)
			c.reconcileMu.Unlock()
			c.wg.Done()
		}()
		_ = c.ReconcileProject(c.ctx, project)
	}()
}

func (c *Catalog) ReconcileProject(ctx context.Context, project Project) error {
	project = normalizeProject(project)
	unlock := c.lockProject(project.Key)
	defer unlock()

	tasks, err := c.store.ListTasks(ctx, project.Root)
	if err != nil {
		return err
	}
	var generation int64
	err = c.db.QueryRowContext(ctx, `UPDATE task_projects SET scan_generation=scan_generation+1,state='scanning',total=? WHERE project_key=? RETURNING scan_generation`, len(tasks), project.Key).Scan(&generation)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := c.upsertSnapshot(ctx, project, task, generation); err != nil {
			continue
		}
	}
	now := time.Now().UnixMilli()
	_, _ = c.db.ExecContext(ctx, `UPDATE task_snapshots SET missing_since=CASE WHEN missing_since=0 THEN ? ELSE missing_since END,health='missing'
        WHERE project_key=? AND seen_generation<>?`, now, project.Key, generation)
	cutoff := now - missingGrace.Milliseconds()
	_, _ = c.db.ExecContext(ctx, `DELETE FROM task_events WHERE project_key=? AND task_id IN (
        SELECT task_id FROM task_snapshots WHERE project_key=? AND seen_generation<>? AND missing_since>0 AND missing_since<=?
    )`, project.Key, project.Key, generation, cutoff)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM task_event_sources WHERE project_key=? AND task_id IN (
        SELECT task_id FROM task_snapshots WHERE project_key=? AND seen_generation<>? AND missing_since>0 AND missing_since<=?
    )`, project.Key, project.Key, generation, cutoff)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM task_snapshots WHERE project_key=? AND seen_generation<>? AND missing_since>0 AND missing_since<=?`,
		project.Key, generation, cutoff)
	tx, beginErr := c.db.BeginTx(ctx, nil)
	if beginErr != nil {
		return beginErr
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_projects SET state='ready',indexed=?,completed_at=? WHERE project_key=?`, len(tasks), now, project.Key); err != nil {
		_ = tx.Rollback()
		return err
	}
	revision, err := bump(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	c.revision.Store(revision)
	c.refresh(context.Background())
	return nil
}

func (c *Catalog) indexSnapshot(ctx context.Context, root, taskID string, generation int64) error {
	project, ok, err := c.projectByRoot(ctx, root)
	if err != nil {
		return err
	}
	if !ok {
		project, err = c.RegisterProject(ctx, root, filepath.Base(root))
		if err != nil {
			return err
		}
	}
	unlock := c.lockProject(project.Key)
	defer unlock()

	task, err := c.store.GetTask(ctx, project.Root, taskID)
	if err != nil || task == nil {
		return err
	}
	return c.upsertSnapshot(ctx, project, *task, generation)
}

func (c *Catalog) upsertSnapshot(ctx context.Context, project Project, task taskmonitor.TaskSnapshot, generation int64) error {
	b, err := json.Marshal(task)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	lease := int64(0)
	if !task.RuntimeLeaseUntil.IsZero() {
		lease = task.RuntimeLeaseUntil.UnixMilli()
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_snapshots(project_key,task_id,session_id,job_id,state,runtime_state,runtime_lease_until,version,
        created_at,updated_at,error_code,snapshot_fingerprint,snapshot_json,health,missing_since,seen_generation) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'ok',0,?)
        ON CONFLICT(project_key,task_id) DO UPDATE SET session_id=excluded.session_id,job_id=excluded.job_id,state=excluded.state,
        runtime_state=excluded.runtime_state,runtime_lease_until=excluded.runtime_lease_until,version=excluded.version,created_at=excluded.created_at,
        updated_at=excluded.updated_at,error_code=excluded.error_code,snapshot_fingerprint=excluded.snapshot_fingerprint,snapshot_json=excluded.snapshot_json,
        health='ok',missing_since=0,seen_generation=excluded.seen_generation`, project.Key, task.TaskID, task.SessionID, task.JobID, task.State,
		task.RuntimeState, lease, task.Version, task.CreatedAt.UnixMilli(), task.UpdatedAt.UnixMilli(), task.ErrorCode, hex.EncodeToString(hash[:]), b, generation)
	if err != nil {
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
	c.revision.Store(revision)
	c.refresh(context.Background())
	return nil
}

func (c *Catalog) indexEvents(ctx context.Context, root, taskID string) error {
	if taskID == "" || filepath.Base(taskID) != taskID || strings.ContainsAny(taskID, `/\\`) {
		return errors.New("invalid task id")
	}
	project, ok, err := c.projectByRoot(ctx, root)
	if err != nil || !ok {
		return err
	}
	unlock := c.lockProject(project.Key)
	defer unlock()

	path := filepath.Join(project.Root, ".reasonix", "tasks", taskID, "events.jsonl")
	info, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil {
		return statErr
	}
	fingerprint := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	var oldPath, oldFingerprint string
	var oldSize, offset int64
	err = c.db.QueryRowContext(ctx, `SELECT path,size,indexed_offset,fingerprint FROM task_event_sources WHERE project_key=? AND task_id=?`,
		project.Key, taskID).Scan(&oldPath, &oldSize, &offset, &oldFingerprint)
	reset := errors.Is(err, sql.ErrNoRows) || oldPath != path || info.Size() < oldSize || info.Size() == oldSize && fingerprint != oldFingerprint
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !reset && info.Size() == oldSize && fingerprint == oldFingerprint {
		return nil
	}
	if reset {
		offset = 0
	}
	tail, err := c.store.ReadEventTail(ctx, project.Root, taskID, offset)
	if err != nil {
		return err
	}
	reset = reset || tail.Reset
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if reset {
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_events WHERE project_key=? AND task_id=?`, project.Key, taskID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, event := range tail.Items {
		b, _ := json.Marshal(event)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO task_events(project_key,task_id,sequence,timestamp,event_type,state,runtime_state,event_json)
			VALUES(?,?,?,?,?,?,?,?)`, project.Key, taskID, event.Sequence, event.Timestamp.UnixMilli(), event.EventType, event.State, event.RuntimeState, b); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_event_sources(project_key,task_id,path,size,indexed_offset,fingerprint,state,error)
		VALUES(?,?,?,?,?,?,'ready','') ON CONFLICT(project_key,task_id) DO UPDATE SET path=excluded.path,size=excluded.size,
		indexed_offset=excluded.indexed_offset,fingerprint=excluded.fingerprint,state='ready',error=''`, project.Key, taskID, path,
		info.Size(), tail.NextOffset, fingerprint)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (c *Catalog) ListPage(ctx context.Context, req PageRequest) (Page, error) {
	status := c.Status()
	out := Page{Items: []Item{}, Revision: c.revision.Load(), Partial: status.Pending > 0, Status: status}
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	cur, err := decodeCursor(req.Cursor)
	if err != nil {
		return out, err
	}
	if cur != nil && cur.Revision != out.Revision {
		out.StaleCursor = true
		return out, nil
	}
	where := []string{`s.health='ok'`, `s.missing_since=0`}
	args := []any{}
	if len(req.ProjectKeys) > 0 {
		parts := make([]string, len(req.ProjectKeys))
		for i, key := range req.ProjectKeys {
			parts[i] = "?"
			args = append(args, key)
		}
		where = append(where, `s.project_key IN (`+strings.Join(parts, ",")+`)`)
	}
	if req.SessionID != "" {
		where = append(where, `s.session_id=?`)
		args = append(args, req.SessionID)
	}
	if len(req.States) > 0 {
		parts := make([]string, len(req.States))
		for i, state := range req.States {
			parts[i] = "?"
			args = append(args, state)
		}
		where = append(where, `s.state IN (`+strings.Join(parts, ",")+`)`)
	}
	if query := strings.ToLower(strings.TrimSpace(req.Query)); query != "" {
		where = append(where, `(lower(s.task_id) LIKE ? OR lower(s.session_id) LIKE ? OR lower(s.error_code) LIKE ?)`)
		like := "%" + query + "%"
		args = append(args, like, like, like)
	}
	if cur != nil {
		where = append(where, `(s.updated_at<? OR (s.updated_at=? AND s.project_key>?) OR (s.updated_at=? AND s.project_key=? AND s.task_id>?))`)
		args = append(args, cur.Updated, cur.Updated, cur.Project, cur.Updated, cur.Project, cur.Task)
	}
	args = append(args, limit+1)
	rows, err := c.db.QueryContext(ctx, `SELECT s.project_key,p.project_label,s.snapshot_json FROM task_snapshots s JOIN task_projects p ON p.project_key=s.project_key
        WHERE `+strings.Join(where, ` AND `)+` ORDER BY s.updated_at DESC,s.project_key,s.task_id LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Item
		var raw []byte
		if err := rows.Scan(&item.ProjectKey, &item.ProjectLabel, &raw); err != nil {
			return out, err
		}
		if json.Unmarshal(raw, &item.Task) != nil {
			continue
		}
		item.Task.ReconcileRuntime(time.Now())
		out.Items = append(out.Items, item)
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		last := out.Items[len(out.Items)-1]
		out.NextCursor = encodeCursor(cursor{Revision: out.Revision, Updated: last.Task.UpdatedAt.UnixMilli(), Project: last.ProjectKey, Task: last.Task.TaskID})
	}
	return out, rows.Err()
}

func (c *Catalog) ListEventPage(ctx context.Context, projectKey, taskID string, after, limit int) (EventPage, error) {
	out := EventPage{Items: []taskmonitor.TaskEvent{}, NextSequence: after}
	project, ok, err := c.Project(ctx, projectKey)
	if err != nil || !ok {
		return out, err
	}
	if err := c.indexEvents(ctx, project.Root, taskID); err != nil {
		out.Partial = true
		return out, nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	rows, err := c.db.QueryContext(ctx, `SELECT event_json FROM task_events WHERE project_key=? AND task_id=? AND sequence>? ORDER BY sequence LIMIT ?`, projectKey, taskID, after, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var event taskmonitor.TaskEvent
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		if json.Unmarshal(raw, &event) == nil {
			out.Items = append(out.Items, event)
			out.NextSequence = event.Sequence
		}
	}
	return out, rows.Err()
}

func (c *Catalog) Project(ctx context.Context, key string) (Project, bool, error) {
	var project Project
	err := c.db.QueryRowContext(ctx, `SELECT project_key,project_root,project_label FROM task_projects WHERE project_key=?`, key).Scan(&project.Key, &project.Root, &project.Label)
	if errors.Is(err, sql.ErrNoRows) {
		return project, false, nil
	}
	return project, err == nil, err
}

func (c *Catalog) projectByRoot(ctx context.Context, root string) (Project, bool, error) {
	return c.Project(ctx, ProjectKey(root))
}

func bump(ctx context.Context, tx *sql.Tx) (uint64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE task_state SET revision=revision+1 WHERE id=1`); err != nil {
		return 0, err
	}
	var revision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM task_state WHERE id=1`).Scan(&revision)
	return revision, err
}

func encodeCursor(value cursor) string {
	b, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(value string) (*cursor, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid task cursor: %w", err)
	}
	var out cursor
	if json.Unmarshal(b, &out) != nil || out.Task == "" {
		return nil, errors.New("invalid task cursor")
	}
	return &out, nil
}

func (c *Catalog) refresh(ctx context.Context) {
	var indexed, total, pending, failed int64
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_snapshots WHERE health='ok'`).Scan(&indexed)
	_ = c.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(total),0) FROM task_projects`).Scan(&total)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_projects WHERE state<>'ready'`).Scan(&pending)
	_ = c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_snapshots WHERE health='corrupt'`).Scan(&failed)
	c.statusMu.Lock()
	c.status.Revision, c.status.Indexed, c.status.Total, c.status.Pending, c.status.Failed = c.revision.Load(), indexed, total, pending, failed
	c.statusMu.Unlock()
}

func (c *Catalog) Status() Status {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}

func (c *Catalog) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		c.cancel()
		c.reconcileMu.Lock()
		c.reconcileDone = true
		c.reconcileMu.Unlock()
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
