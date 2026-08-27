// Package topicstate persists authoritative Desktop topic metadata in SQLite.
// Unlike projection databases, a Store owns user-visible state and must never
// silently fall back to an empty in-memory database.
package topicstate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	moderncsqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const schemaVersion = 1

// FutureSchemaError means a newer Reasonix version owns this database. Callers
// must leave the file untouched and may only use an explicitly compatible
// legacy read path.
type FutureSchemaError struct {
	Found     int
	Supported int
}

func (e *FutureSchemaError) Error() string {
	return fmt.Sprintf("topic state schema %d is newer than supported %d", e.Found, e.Supported)
}

// Record is the complete durable metadata for one Desktop topic. AutoMeta is
// retained as raw JSON so a read/modify/write by this version preserves fields
// introduced by a future compatible writer.
type Record struct {
	TopicID     string
	Title       string
	TitleSource string
	CreatedAtMS int64
	AutoMeta    json.RawMessage
	RowRevision int64
	UpdatedAtMS int64
}

// State tracks the authoritative revision and the legacy JSON mirror outbox.
type State struct {
	Revision               int64
	LegacyBridge           bool
	LegacyExportedRevision int64
	LegacyPendingRevision  int64
	LegacyTitlesDigest     string
	LegacySourcesDigest    string
	LegacyCreatedAtsDigest string
	LegacyAutoMetaDigest   string
}

// Snapshot is one transactionally consistent view of all topic metadata.
type Snapshot struct {
	Records map[string]Record
	State   State
}

// Store is a single-scope SQLite topic database.
type Store struct {
	path string
	db   *sql.DB
	now  func() time.Time
}

// Open opens or creates a durable topic database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("topic state path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create topic state directory: %w", err)
	}
	_ = os.Chmod(filepath.Dir(path), 0o700)

	db, err := sql.Open("sqlite", diskFileDSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return fail(err)
	}
	for _, pragma := range []string{`PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=2000`} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fail(err)
		}
	}
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil {
		return fail(err)
	}
	if integrity != "ok" {
		return fail(fmt.Errorf("topic state quick check: %s", integrity))
	}
	if err := applyMigrations(ctx, db, time.Now); err != nil {
		return fail(err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return fail(err)
	}
	for _, pragma := range []string{`PRAGMA synchronous=FULL`, `PRAGMA secure_delete=ON`} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fail(err)
		}
	}
	_ = os.Chmod(path, 0o600)
	_ = os.Chmod(path+"-wal", 0o600)
	_ = os.Chmod(path+"-shm", 0o600)
	return &Store{path: path, db: db, now: time.Now}, nil
}

func diskFileDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && len(slash) >= 2 && slash[1] == ':' {
		slash = "/" + slash
	}
	u := &url.URL{Scheme: "file", Path: slash}
	return u.String() + "?_pragma=busy_timeout%282000%29&_pragma=foreign_keys%281%29"
}

func applyMigrations(ctx context.Context, db *sql.DB, now func() time.Time) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        applied_at INTEGER NOT NULL
    )`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	if current > schemaVersion {
		return &FutureSchemaError{Found: current, Supported: schemaVersion}
	}
	if current == schemaVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE topics (
        topic_id TEXT PRIMARY KEY,
        title TEXT NOT NULL DEFAULT '',
        title_source TEXT NOT NULL DEFAULT '',
        created_at_ms INTEGER NOT NULL DEFAULT 0,
        auto_meta_json TEXT NOT NULL DEFAULT '',
        row_revision INTEGER NOT NULL DEFAULT 0,
        updated_at_ms INTEGER NOT NULL DEFAULT 0
    )`); err != nil {
		return fmt.Errorf("create topics table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE store_state (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        revision INTEGER NOT NULL DEFAULT 0,
        legacy_bridge INTEGER NOT NULL DEFAULT 0,
        legacy_exported_revision INTEGER NOT NULL DEFAULT 0,
        legacy_pending_revision INTEGER NOT NULL DEFAULT 0,
        legacy_titles_digest TEXT NOT NULL DEFAULT '',
        legacy_sources_digest TEXT NOT NULL DEFAULT '',
        legacy_created_ats_digest TEXT NOT NULL DEFAULT '',
        legacy_auto_meta_digest TEXT NOT NULL DEFAULT ''
    )`); err != nil {
		return fmt.Errorf("create store state table: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO store_state(id) VALUES(1)`); err != nil {
		return fmt.Errorf("initialize store state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, schemaVersion, now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// Close releases the underlying SQLite handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the database path for diagnostics and lock coordination.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Snapshot returns all records and bridge state from one read transaction.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	records, err := readRecords(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Records: records, State: state}, nil
}

// SetLegacyBridge enables the compatibility outbox permanently. Enabling it
// marks the current revision pending so the caller must publish a full mirror.
func (s *Store) SetLegacyBridge(ctx context.Context) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return State{}, err
	}
	if !state.LegacyBridge {
		state.LegacyBridge = true
		state.LegacyPendingRevision = state.Revision
		if _, err := tx.ExecContext(ctx, `UPDATE store_state SET legacy_bridge=1, legacy_pending_revision=? WHERE id=1`, state.LegacyPendingRevision); err != nil {
			return State{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}

// MarkLegacyExported acknowledges a complete legacy mirror. It only clears the
// outbox when expectedRevision is still the current authoritative revision.
func (s *Store) MarkLegacyExported(ctx context.Context, expectedRevision int64, digests [4]string) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return State{}, err
	}
	if state.Revision == expectedRevision {
		state.LegacyExportedRevision = expectedRevision
		state.LegacyPendingRevision = 0
		state.LegacyTitlesDigest = digests[0]
		state.LegacySourcesDigest = digests[1]
		state.LegacyCreatedAtsDigest = digests[2]
		state.LegacyAutoMetaDigest = digests[3]
		if _, err := tx.ExecContext(ctx, `UPDATE store_state SET
            legacy_exported_revision=?, legacy_pending_revision=0,
            legacy_titles_digest=?, legacy_sources_digest=?,
            legacy_created_ats_digest=?, legacy_auto_meta_digest=?
            WHERE id=1`, expectedRevision, digests[0], digests[1], digests[2], digests[3]); err != nil {
			return State{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}

// Update atomically changes one topic. Empty records are removed.
func (s *Store) Update(ctx context.Context, topicID string, mutate func(*Record)) (State, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return State{}, errors.New("topic id is empty")
	}
	return s.mutateOne(ctx, topicID, mutate)
}

// Delete removes all metadata for one topic in a single transaction.
func (s *Store) Delete(ctx context.Context, topicID string) (State, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return s.currentState(ctx)
	}
	return s.mutateOne(ctx, topicID, func(record *Record) {
		*record = Record{TopicID: topicID}
	})
}

func (s *Store) mutateOne(ctx context.Context, topicID string, mutate func(*Record)) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return State{}, err
	}
	record := Record{TopicID: topicID}
	var autoMeta string
	err = tx.QueryRowContext(ctx, `SELECT title, title_source, created_at_ms,
        auto_meta_json, row_revision, updated_at_ms FROM topics WHERE topic_id=?`, topicID).Scan(
		&record.Title, &record.TitleSource, &record.CreatedAtMS, &autoMeta,
		&record.RowRevision, &record.UpdatedAtMS,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return State{}, err
	}
	if autoMeta != "" {
		record.AutoMeta = json.RawMessage(autoMeta)
	}
	before := cloneRecord(record)
	mutate(&record)
	record.TopicID = topicID
	normalizeRecord(&record)
	if recordEqual(before, record) {
		if err := tx.Commit(); err != nil {
			return State{}, err
		}
		return state, nil
	}
	state.Revision++
	if recordEmpty(record) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM topics WHERE topic_id=?`, topicID); err != nil {
			return State{}, err
		}
	} else {
		record.RowRevision = state.Revision
		record.UpdatedAtMS = s.now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `INSERT INTO topics(
            topic_id, title, title_source, created_at_ms, auto_meta_json,
            row_revision, updated_at_ms) VALUES(?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(topic_id) DO UPDATE SET
            title=excluded.title, title_source=excluded.title_source,
            created_at_ms=excluded.created_at_ms, auto_meta_json=excluded.auto_meta_json,
            row_revision=excluded.row_revision, updated_at_ms=excluded.updated_at_ms`,
			record.TopicID, record.Title, record.TitleSource, record.CreatedAtMS,
			string(record.AutoMeta), record.RowRevision, record.UpdatedAtMS); err != nil {
			return State{}, err
		}
	}
	if state.LegacyBridge {
		state.LegacyPendingRevision = state.Revision
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_state SET revision=?, legacy_pending_revision=? WHERE id=1`, state.Revision, state.LegacyPendingRevision); err != nil {
		return State{}, err
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}

// ReplaceAll transactionally replaces the complete record set. It is intended
// for first migration and deterministic legacy reconciliation.
func (s *Store) ReplaceAll(ctx context.Context, records map[string]Record) (State, error) {
	return s.mutate(ctx, func(current map[string]Record) {
		clear(current)
		for id, record := range records {
			record.TopicID = strings.TrimSpace(id)
			normalizeRecord(&record)
			if record.TopicID != "" && !recordEmpty(record) {
				current[record.TopicID] = cloneRecord(record)
			}
		}
	})
}

// ReplaceTitles replaces only the title field while preserving every other
// field, including unknown auto-title metadata.
func (s *Store) ReplaceTitles(ctx context.Context, values map[string]string) (State, error) {
	return s.replaceField(ctx, func(records map[string]Record) {
		for id, record := range records {
			record.Title = ""
			records[id] = record
		}
		for id, value := range values {
			record := records[id]
			record.TopicID, record.Title = id, value
			records[id] = record
		}
	})
}

// ReplaceSources replaces only the title source field.
func (s *Store) ReplaceSources(ctx context.Context, values map[string]string) (State, error) {
	return s.replaceField(ctx, func(records map[string]Record) {
		for id, record := range records {
			record.TitleSource = ""
			records[id] = record
		}
		for id, value := range values {
			record := records[id]
			record.TopicID, record.TitleSource = id, value
			records[id] = record
		}
	})
}

// MergeMissingTitleIndex fills title-index gaps from a scan-built snapshot
// without replacing values written after that scan began. Background session
// repair uses this compare-at-commit behavior so a concurrent manual rename
// cannot be reverted by stale whole-map data.
func (s *Store) MergeMissingTitleIndex(ctx context.Context, titles, sources map[string]string, deleted map[string]bool) (State, error) {
	return s.replaceField(ctx, func(records map[string]Record) {
		for id := range deleted {
			delete(records, id)
		}
		for id, value := range titles {
			if deleted[id] {
				continue
			}
			record := records[id]
			if strings.TrimSpace(record.Title) != "" {
				continue
			}
			record.TopicID, record.Title = id, value
			records[id] = record
		}
		for id, value := range sources {
			if deleted[id] {
				continue
			}
			record := records[id]
			if strings.TrimSpace(record.TitleSource) != "" {
				continue
			}
			record.TopicID, record.TitleSource = id, value
			records[id] = record
		}
	})
}

// ReplaceCreatedAts replaces only topic creation timestamps.
func (s *Store) ReplaceCreatedAts(ctx context.Context, values map[string]int64) (State, error) {
	return s.replaceField(ctx, func(records map[string]Record) {
		for id, record := range records {
			record.CreatedAtMS = 0
			records[id] = record
		}
		for id, value := range values {
			record := records[id]
			record.TopicID, record.CreatedAtMS = id, value
			records[id] = record
		}
	})
}

// ReplaceAutoMeta replaces only raw automatic-title metadata.
func (s *Store) ReplaceAutoMeta(ctx context.Context, values map[string]json.RawMessage) (State, error) {
	return s.replaceField(ctx, func(records map[string]Record) {
		for id, record := range records {
			record.AutoMeta = nil
			records[id] = record
		}
		for id, value := range values {
			record := records[id]
			record.TopicID, record.AutoMeta = id, append(json.RawMessage(nil), value...)
			records[id] = record
		}
	})
}

func (s *Store) replaceField(ctx context.Context, mutate func(map[string]Record)) (State, error) {
	return s.mutate(ctx, func(records map[string]Record) {
		mutate(records)
		for id, record := range records {
			normalizeRecord(&record)
			if recordEmpty(record) {
				delete(records, id)
			} else {
				records[id] = record
			}
		}
	})
}

func (s *Store) mutate(ctx context.Context, mutate func(map[string]Record)) (State, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := readState(ctx, tx)
	if err != nil {
		return State{}, err
	}
	records, err := readRecords(ctx, tx)
	if err != nil {
		return State{}, err
	}
	before := cloneRecords(records)
	mutate(records)
	if recordsEqual(before, records) {
		if err := tx.Commit(); err != nil {
			return State{}, err
		}
		return state, nil
	}
	nowMS := s.now().UnixMilli()
	state.Revision++
	for id, record := range records {
		if previous, ok := before[id]; !ok || !recordEqual(previous, record) {
			record.RowRevision = state.Revision
			record.UpdatedAtMS = nowMS
			records[id] = record
		}
	}
	if err := writeRecords(ctx, tx, records); err != nil {
		return State{}, err
	}
	if state.LegacyBridge {
		state.LegacyPendingRevision = state.Revision
	}
	if _, err := tx.ExecContext(ctx, `UPDATE store_state SET revision=?, legacy_pending_revision=? WHERE id=1`, state.Revision, state.LegacyPendingRevision); err != nil {
		return State{}, err
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s *Store) currentState(ctx context.Context) (State, error) {
	return readState(ctx, s.db)
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readState(ctx context.Context, q rowQuerier) (State, error) {
	var state State
	var bridge int
	err := q.QueryRowContext(ctx, `SELECT revision, legacy_bridge,
        legacy_exported_revision, legacy_pending_revision,
        legacy_titles_digest, legacy_sources_digest,
        legacy_created_ats_digest, legacy_auto_meta_digest
        FROM store_state WHERE id=1`).Scan(
		&state.Revision, &bridge, &state.LegacyExportedRevision,
		&state.LegacyPendingRevision, &state.LegacyTitlesDigest,
		&state.LegacySourcesDigest, &state.LegacyCreatedAtsDigest,
		&state.LegacyAutoMetaDigest,
	)
	state.LegacyBridge = bridge != 0
	return state, err
}

type rowsQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readRecords(ctx context.Context, q rowsQuerier) (map[string]Record, error) {
	rows, err := q.QueryContext(ctx, `SELECT topic_id, title, title_source,
        created_at_ms, auto_meta_json, row_revision, updated_at_ms FROM topics`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := map[string]Record{}
	for rows.Next() {
		var record Record
		var autoMeta string
		if err := rows.Scan(&record.TopicID, &record.Title, &record.TitleSource,
			&record.CreatedAtMS, &autoMeta, &record.RowRevision, &record.UpdatedAtMS); err != nil {
			return nil, err
		}
		if autoMeta != "" {
			record.AutoMeta = json.RawMessage(autoMeta)
		}
		records[record.TopicID] = record
	}
	return records, rows.Err()
}

func writeRecords(ctx context.Context, tx *sql.Tx, records map[string]Record) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM topics`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO topics(
        topic_id, title, title_source, created_at_ms, auto_meta_json,
        row_revision, updated_at_ms) VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		record := records[id]
		normalizeRecord(&record)
		if recordEmpty(record) {
			continue
		}
		if _, err := stmt.ExecContext(ctx, record.TopicID, record.Title,
			record.TitleSource, record.CreatedAtMS, string(record.AutoMeta),
			record.RowRevision, record.UpdatedAtMS); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRecord(record *Record) {
	record.TopicID = strings.TrimSpace(record.TopicID)
	record.Title = strings.TrimSpace(record.Title)
	record.TitleSource = strings.TrimSpace(record.TitleSource)
	if record.CreatedAtMS < 0 {
		record.CreatedAtMS = 0
	}
	if len(bytes.TrimSpace(record.AutoMeta)) == 0 || bytes.Equal(bytes.TrimSpace(record.AutoMeta), []byte("null")) || bytes.Equal(bytes.TrimSpace(record.AutoMeta), []byte("{}")) {
		record.AutoMeta = nil
	} else {
		record.AutoMeta = append(json.RawMessage(nil), bytes.TrimSpace(record.AutoMeta)...)
	}
}

func recordEmpty(record Record) bool {
	return record.Title == "" && record.TitleSource == "" && record.CreatedAtMS <= 0 && len(record.AutoMeta) == 0
}

func cloneRecord(record Record) Record {
	record.AutoMeta = append(json.RawMessage(nil), record.AutoMeta...)
	return record
}

func cloneRecords(records map[string]Record) map[string]Record {
	clone := make(map[string]Record, len(records))
	for id, record := range records {
		clone[id] = cloneRecord(record)
	}
	return clone
}

func recordsEqual(a, b map[string]Record) bool {
	if len(a) != len(b) {
		return false
	}
	for id, record := range a {
		if other, ok := b[id]; !ok || !recordEqual(record, other) {
			return false
		}
	}
	return true
}

func recordEqual(a, b Record) bool {
	return a.TopicID == b.TopicID && a.Title == b.Title &&
		a.TitleSource == b.TitleSource && a.CreatedAtMS == b.CreatedAtMS &&
		bytes.Equal(a.AutoMeta, b.AutoMeta)
}

// IsCorruptionError reports only integrity-level failures. Busy, permissions,
// IO errors, and future schemas must never cause an authoritative DB rename.
func IsCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	var future *FutureSchemaError
	if errors.As(err, &future) {
		return false
	}
	var sqliteErr *moderncsqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 0xff {
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			return true
		}
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "quick check") ||
		strings.Contains(message, "malformed") ||
		strings.Contains(message, "file is not a database") ||
		strings.Contains(message, "not a database")
}

// Quarantine preserves a corrupt database and its WAL sidecars for diagnosis.
func Quarantine(path string, now time.Time) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("topic state path is empty")
	}
	quarantined := fmt.Sprintf("%s.corrupt-%d", path, now.UnixMilli())
	if err := os.Rename(path, quarantined); err != nil {
		return "", err
	}
	moved := []string{}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Rename(path+suffix, quarantined+suffix); err == nil {
			moved = append(moved, suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			for _, movedSuffix := range moved {
				_ = os.Rename(quarantined+movedSuffix, path+movedSuffix)
			}
			_ = os.Rename(quarantined, path)
			return "", err
		}
	}
	return quarantined, nil
}
