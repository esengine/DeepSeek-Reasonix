package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/projectiondb"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

const searchIndexVersion = 4

var searchMigrations = []projectiondb.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE documents (rowid INTEGER PRIMARY KEY, message_id TEXT NOT NULL, version INTEGER NOT NULL, position INTEGER NOT NULL, event_sequence INTEGER NOT NULL, valid_to INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL, preview TEXT NOT NULL, text TEXT NOT NULL, current INTEGER NOT NULL, UNIQUE(message_id,version))`,
		`CREATE INDEX documents_snapshot_position ON documents(position DESC,event_sequence,valid_to)`,
		`CREATE INDEX documents_current_id ON documents(message_id) WHERE current=1`,
		`CREATE VIRTUAL TABLE documents_fts USING fts5(text, content='documents', content_rowid='rowid', tokenize='trigram')`,
		`CREATE TRIGGER documents_ai AFTER INSERT ON documents BEGIN INSERT INTO documents_fts(rowid,text) VALUES (new.rowid,new.text); END`,
		`CREATE TRIGGER documents_ad AFTER DELETE ON documents BEGIN INSERT INTO documents_fts(documents_fts,rowid,text) VALUES('delete',old.rowid,old.text); END`,
		`CREATE TRIGGER documents_au AFTER UPDATE OF text ON documents BEGIN INSERT INTO documents_fts(documents_fts,rowid,text) VALUES('delete',old.rowid,old.text); INSERT INTO documents_fts(rowid,text) VALUES(new.rowid,new.text); END`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}}}

type searchBuildState struct {
	positions    map[string]int64
	versions     map[string]int
	nextPosition int64
}

type searchPreparation struct {
	done chan struct{}
	err  error
}

func searchIndexPath(root, sessionID string) string {
	return filepath.Join(root, ".query-cache", filepath.Base(sessionID), "search-v1.sqlite")
}

func (q *Query) SearchHistory(ctx context.Context, ref SessionRef, textQuery, cursor string, limit int) (SearchHistoryPage, error) {
	if q == nil {
		return SearchHistoryPage{}, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return SearchHistoryPage{}, err
	}
	textQuery = strings.TrimSpace(textQuery)
	if textQuery == "" {
		return SearchHistoryPage{}, errors.New("session: history search query is required")
	}
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 200)
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return SearchHistoryPage{}, errors.New("session: history search requires filesystem persistence")
	}
	path := searchIndexPath(filesystem.Root, ref.SessionID)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		preparation := q.prepareSearchIndex(filesystem, ref.SessionID, path)
		select {
		case <-preparation.done:
			if preparation.err != nil {
				return SearchHistoryPage{Status: "failed"}, preparation.err
			}
		default:
			return SearchHistoryPage{Status: "preparing"}, nil
		}
	}
	lock := q.projectionLock("search", ref.SessionID)
	lock.Lock()
	err := ensureSearchIndex(ctx, filesystem, ref.SessionID, path)
	lock.Unlock()
	if err != nil {
		return SearchHistoryPage{}, err
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: searchMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return SearchHistoryPage{}, err
	}
	defer handle.DB.Close()
	metadata, err := readSearchMetadata(ctx, handle.DB)
	if err != nil {
		return SearchHistoryPage{}, err
	}
	snapshot := metadata.durableSequence
	before := int64(^uint64(0) >> 1)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(textQuery)))
	if cursor != "" {
		parsed, err := decodeSearchHistoryCursor(cursor)
		if err != nil {
			return SearchHistoryPage{}, err
		}
		if parsed.SessionID != ref.SessionID || parsed.StorageRevision != metadata.storageRevision || parsed.Projection != searchIndexVersion || parsed.QueryDigest != digest || parsed.SnapshotSequence > snapshot || parsed.BeforePosition <= 0 || parsed.Generation != metadata.generation {
			return SearchHistoryPage{Status: "stale_cursor", CoverageSequence: metadata.durableSequence}, nil
		}
		snapshot, before = parsed.SnapshotSequence, parsed.BeforePosition
	}
	var rows *sql.Rows
	if utf8.RuneCountInString(textQuery) >= 3 {
		match := `"` + strings.ReplaceAll(textQuery, `"`, `""`) + `"`
		rows, err = handle.DB.QueryContext(ctx, `SELECT d.message_id,d.position,d.role,d.preview,d.event_sequence FROM documents_fts JOIN documents d ON d.rowid=documents_fts.rowid WHERE documents_fts MATCH ? AND instr(d.text,?)>0 AND d.position<? AND d.event_sequence<=? AND (d.valid_to=0 OR d.valid_to>?) ORDER BY d.position DESC LIMIT ?`, match, textQuery, before, snapshot, snapshot, limit+1)
	} else {
		rows, err = handle.DB.QueryContext(ctx, `SELECT message_id,position,role,preview,event_sequence FROM documents WHERE instr(text,?)>0 AND position<? AND event_sequence<=? AND (valid_to=0 OR valid_to>?) ORDER BY position DESC LIMIT ?`, textQuery, before, snapshot, snapshot, limit+1)
	}
	if err != nil {
		return SearchHistoryPage{}, err
	}
	defer rows.Close()
	page := SearchHistoryPage{Hits: []SearchHistoryHit{}, SnapshotSequence: snapshot, CoverageSequence: metadata.durableSequence, Status: "ready"}
	for rows.Next() {
		var hit SearchHistoryHit
		if err := rows.Scan(&hit.MessageID, &hit.Position, &hit.Role, &hit.Preview, &hit.EventSequence); err != nil {
			return SearchHistoryPage{}, err
		}
		if len(page.Hits) == limit {
			page.HasMore = true
			break
		}
		page.Hits = append(page.Hits, hit)
	}
	if err := rows.Err(); err != nil {
		return SearchHistoryPage{}, err
	}
	if page.HasMore && len(page.Hits) > 0 {
		page.NextCursor, err = encodeSearchHistoryCursor(searchHistoryCursor{SessionID: ref.SessionID, StorageRevision: metadata.storageRevision, SnapshotSequence: snapshot, BeforePosition: page.Hits[len(page.Hits)-1].Position, Projection: searchIndexVersion, QueryDigest: digest, Generation: metadata.generation})
		if err != nil {
			return SearchHistoryPage{}, err
		}
	}
	return page, nil
}

func (q *Query) prepareSearchIndex(filesystem *FilesystemPersistence, sessionID, path string) *searchPreparation {
	q.searchMu.Lock()
	if current := q.searchBuilds[sessionID]; current != nil {
		q.searchMu.Unlock()
		return current
	}
	preparation := &searchPreparation{done: make(chan struct{})}
	q.searchBuilds[sessionID] = preparation
	q.searchMu.Unlock()
	go func() {
		if err := q.slots.acquire(q.rebuildCtx, rebuildPrioritySearch); err != nil {
			preparation.err = err
			close(preparation.done)
			return
		}
		defer q.slots.release()
		lock := q.projectionLock("search", sessionID)
		lock.Lock()
		preparation.err = ensureSearchIndex(q.rebuildCtx, filesystem, sessionID, path)
		lock.Unlock()
		close(preparation.done)
	}()
	return preparation
}

type searchMetadata struct {
	sessionID       string
	logSize         int64
	storageRevision int
	projection      int
	durableSequence uint64
	generation      string
}

func readSearchMetadata(ctx context.Context, db *sql.DB) (searchMetadata, error) {
	values := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT key,value FROM metadata`)
	if err != nil {
		return searchMetadata{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return searchMetadata{}, err
		}
		values[key] = value
	}
	metadata := searchMetadata{sessionID: values["session_id"], generation: values["generation"]}
	if _, err := fmt.Sscan(values["log_size"], &metadata.logSize); err != nil {
		return searchMetadata{}, err
	}
	if _, err := fmt.Sscan(values["storage_revision"], &metadata.storageRevision); err != nil {
		return searchMetadata{}, err
	}
	if _, err := fmt.Sscan(values["projection_version"], &metadata.projection); err != nil {
		return searchMetadata{}, err
	}
	if _, err := fmt.Sscan(values["durable_sequence"], &metadata.durableSequence); err != nil {
		return searchMetadata{}, err
	}
	return metadata, rows.Err()
}

func ensureSearchIndex(ctx context.Context, persistence *FilesystemPersistence, sessionID, path string) error {
	dir := filepath.Join(persistence.Root, sessionID)
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	revision, err := revisionOfLog(dir)
	if err != nil {
		return err
	}
	if handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: searchMigrations, RequireDisk: true, MaxOpenConns: 1}); err == nil {
		metadata, metaErr := readSearchMetadata(ctx, handle.DB)
		_ = handle.DB.Close()
		if metaErr == nil && metadata.sessionID == sessionID && metadata.storageRevision == manifest.StorageRevision && metadata.projection == searchIndexVersion {
			if metadata.logSize == revision.Size {
				return nil
			}
			if metadata.logSize >= 0 && metadata.logSize < revision.Size {
				if err := incrementSearchIndex(ctx, dir, path, revision, metadata); err == nil {
					return nil
				}
			}
		}
	}
	return rebuildSearchIndex(ctx, dir, path, sessionID, revision)
}

func rebuildSearchIndex(ctx context.Context, dir, path, sessionID string, revision logRevision) error {
	return projectiondb.Rebuild(ctx, projectiondb.OpenOptions{Path: path, Migrations: searchMigrations, RequireDisk: true, MaxOpenConns: 1, QuickCheck: true}, func(ctx context.Context, db *sql.DB) error {
		if err := configureHistoryRebuild(ctx, db); err != nil {
			return err
		}
		manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
		if err != nil {
			return err
		}
		metadata := searchMetadata{sessionID: sessionID, storageRevision: manifest.StorageRevision, projection: searchIndexVersion, generation: randomID()}
		return populateSearchIndex(ctx, dir, db, 0, 1, revision, metadata, searchBuildState{positions: map[string]int64{}, versions: map[string]int{}})
	})
}

func incrementSearchIndex(ctx context.Context, dir, path string, revision logRevision, metadata searchMetadata) error {
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: searchMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return err
	}
	defer handle.DB.Close()
	state := searchBuildState{positions: map[string]int64{}, versions: map[string]int{}}
	rows, err := handle.DB.QueryContext(ctx, `SELECT message_id,position,version FROM documents WHERE current=1`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var position int64
		var version int
		if err := rows.Scan(&id, &position, &version); err != nil {
			_ = rows.Close()
			return err
		}
		state.positions[id], state.versions[id] = position, version
		state.nextPosition = max(state.nextPosition, position)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	versionRows, err := handle.DB.QueryContext(ctx, `SELECT message_id,MAX(version) FROM documents GROUP BY message_id`)
	if err != nil {
		return err
	}
	for versionRows.Next() {
		var id string
		var version int
		if err := versionRows.Scan(&id, &version); err != nil {
			_ = versionRows.Close()
			return err
		}
		state.versions[id] = version
	}
	if err := errors.Join(versionRows.Err(), versionRows.Close()); err != nil {
		return err
	}
	return populateSearchIndex(ctx, dir, handle.DB, metadata.logSize, metadata.durableSequence+1, revision, metadata, state)
}

func populateSearchIndex(ctx context.Context, dir string, db *sql.DB, startOffset int64, nextSequence uint64, revision logRevision, metadata searchMetadata, state searchBuildState) error {
	manifest, err := readManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	log, err := os.Open(logPathForManifest(dir, manifest))
	if err != nil {
		return err
	}
	defer log.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	content := contentStoreForSessionDir(dir)
	var buildErr error
	progress, err := scanHistoryLog(ctx, log, startOffset, nextSequence, revision.Size, content, func(commit Commit) bool {
		for _, event := range commit.Events {
			if err := indexSearchEvent(ctx, tx, content, &state, event); err != nil {
				buildErr = err
				return false
			}
		}
		return true
	})
	if err != nil || buildErr != nil {
		return errors.Join(err, buildErr)
	}
	values := map[string]string{
		"session_id": metadata.sessionID, "log_size": fmt.Sprint(progress.end),
		"log_mtime_ns": fmt.Sprint(progress.modTimeNS), "storage_revision": fmt.Sprint(metadata.storageRevision),
		"projection_version": fmt.Sprint(searchIndexVersion), "durable_sequence": fmt.Sprint(progress.sequence),
		"generation": metadata.generation,
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func indexSearchEvent(ctx context.Context, tx *sql.Tx, content *sessioncontent.Store, state *searchBuildState, event Event) error {
	if event.Kind != "message/complete" && event.Kind != "message/upsert" && event.Kind != "message/retract" && event.Kind != "history/replace" && event.Kind != "legacy/import" {
		return nil
	}
	payload := event.Payload
	if event.PayloadRef != nil {
		var err error
		payload, err = resolveContentPayload(ctx, content, *event.PayloadRef)
		if err != nil {
			return err
		}
	}
	switch event.Kind {
	case "message/retract":
		ids, err := retractedMessageIDs(event, payload)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE documents SET current=0,valid_to=? WHERE message_id=? AND current=1`, event.Sequence, id); err != nil {
				return err
			}
			delete(state.positions, id)
		}
		return nil
	case "message/complete", "message/upsert":
		var body struct {
			Message *provider.Message `json:"message"`
		}
		if err := strictPayload(payload, &body); err != nil || body.Message == nil {
			return damagedPayload(event, err)
		}
		return indexSearchMessage(ctx, tx, state, *body.Message, event.Sequence, event.Kind == "message/upsert")
	case "history/replace", "legacy/import":
		messages, err := replacementEventMessages(event, payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET current=0,valid_to=? WHERE current=1`, event.Sequence); err != nil {
			return err
		}
		state.positions = map[string]int64{}
		state.nextPosition = 0
		for _, message := range messages {
			if err := indexSearchMessage(ctx, tx, state, message, event.Sequence, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func indexSearchMessage(ctx context.Context, tx *sql.Tx, state *searchBuildState, message provider.Message, sequence uint64, upsert bool) error {
	id := strings.TrimSpace(message.ID)
	if id == "" {
		return errors.New("session: search document has no stable message id")
	}
	position, exists := state.positions[id]
	if !exists {
		state.nextPosition++
		position = state.nextPosition
		state.positions[id] = position
	} else if !upsert {
		return fmt.Errorf("session: duplicate search message id %q", id)
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET current=0,valid_to=? WHERE message_id=? AND current=1`, sequence, id); err != nil {
			return err
		}
	}
	version := state.versions[id] + 1
	state.versions[id] = version
	_, err := tx.ExecContext(ctx, `INSERT INTO documents(message_id,version,position,event_sequence,valid_to,role,preview,text,current) VALUES(?,?,?,?,0,?,?,?,1)`, id, version, position, sequence, string(message.Role), messagePreview(message), messageSearchText(message))
	return err
}
