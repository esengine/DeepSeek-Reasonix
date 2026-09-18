package session

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

	"reasonix/internal/agent"
	"reasonix/internal/projectiondb"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

func (q *Query) prepareHistoryIndex(ctx context.Context, ref SessionRef) (*FilesystemPersistence, string, error) {
	if q == nil {
		return nil, "", errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return nil, "", err
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, "", errors.New("session: history index requires filesystem persistence")
	}
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	lock := q.projectionLock("history", ref.SessionID)
	lock.Lock()
	err := ensureHistoryIndex(ctx, filesystem, ref.SessionID, path)
	lock.Unlock()
	if err != nil {
		return nil, "", err
	}
	return filesystem, path, nil
}

func (q *Query) ReadContent(ctx context.Context, ref SessionRef, contentRef sessioncontent.Ref, offset, length int64) ([]byte, error) {
	if q == nil {
		return nil, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return nil, err
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return nil, errors.New("session: content reads require filesystem persistence")
	}
	if offset < 0 || length < 0 || length > 1<<20 || offset > contentRef.Bytes || length > contentRef.Bytes-offset {
		return nil, errors.New("session: invalid or oversized content range")
	}
	if !q.contentAuthorized(ref.SessionID, contentRef.Digest, contentRef.Bytes, contentRef.IndexDigest) {
		return nil, errors.New("session: content reference is not authorized for this session")
	}
	return contentStoreForSessionDir(filepath.Join(filesystem.Root, ref.SessionID)).ReadRange(ctx, contentRef, offset, length)
}

func ensureHistoryIndex(ctx context.Context, persistence *FilesystemPersistence, sessionID, path string) error {
	dir := filepath.Join(persistence.Root, sessionID)
	revision, err := revisionOfLog(dir)
	if err != nil {
		return err
	}
	if historyIndexCurrent(ctx, dir, path, sessionID, revision) {
		return nil
	}
	if updated, err := incrementHistoryIndex(ctx, dir, path, sessionID, revision); updated || (err != nil && !errors.Is(err, ErrDamagedStore)) {
		return err
	}
	// A bad derived checkpoint is not proof that the authoritative log is bad.
	// Rebuild validates the complete log before replacing the old index; genuine
	// corruption still fails and leaves the previous index intact.
	return rebuildHistoryIndex(ctx, dir, path, sessionID, revision)
}

type historyIndexMetadata struct {
	sessionID       string
	logSize         int64
	storageRevision int
	projection      int
	durableSequence uint64
	viewSequence    uint64
	generation      string
}

func readHistoryIndexMetadata(ctx context.Context, db *sql.DB) (historyIndexMetadata, error) {
	values := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT key,value FROM metadata WHERE key IN ('session_id','log_size','storage_revision','projection_version','durable_sequence','history_view_sequence','generation')`)
	if err != nil {
		return historyIndexMetadata{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return historyIndexMetadata{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return historyIndexMetadata{}, err
	}
	metadata := historyIndexMetadata{sessionID: values["session_id"], generation: values["generation"]}
	if _, err := fmt.Sscan(values["log_size"], &metadata.logSize); err != nil {
		return historyIndexMetadata{}, err
	}
	if _, err := fmt.Sscan(values["storage_revision"], &metadata.storageRevision); err != nil {
		return historyIndexMetadata{}, err
	}
	if _, err := fmt.Sscan(values["projection_version"], &metadata.projection); err != nil {
		return historyIndexMetadata{}, err
	}
	if _, err := fmt.Sscan(values["durable_sequence"], &metadata.durableSequence); err != nil {
		return historyIndexMetadata{}, err
	}
	if values["history_view_sequence"] != "" {
		if _, err := fmt.Sscan(values["history_view_sequence"], &metadata.viewSequence); err != nil {
			return historyIndexMetadata{}, err
		}
	}
	return metadata, nil
}

// incrementHistoryIndex advances only the complete transactions appended after
// the published coverage watermark. It returns updated=false when the existing
// file cannot be trusted as a base and must be rebuilt atomically.
func incrementHistoryIndex(ctx context.Context, dir, path, sessionID string, revision logRevision) (updated bool, result error) {
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return false, nil
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return false, nil
	}
	generation, err := historyProjectionGeneration(dir, 0)
	if err != nil {
		return false, err
	}
	manifest, err := readManifest(filepath.Join(dir, "manifest.json"))
	if err != nil || manifest.Codec != Codec {
		return false, nil
	}
	if !metadata.canIncrement(sessionID, revision, generation, manifest.StorageRevision) {
		return false, nil
	}
	log, err := os.Open(logPathForManifest(dir, manifest))
	if err != nil {
		return false, err
	}
	defer log.Close()

	state, err := loadCurrentHistoryState(ctx, handle.DB)
	if err != nil {
		return false, nil
	}

	tx, err := handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	state.tx = tx
	state.statements, err = prepareHistoryBuildStatements(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return false, err
	}
	defer func() {
		state.statements.close()
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	content := contentStoreForSessionDir(dir)
	viewSequence := metadata.viewSequence
	var buildErr error
	progress, err := scanHistoryLog(ctx, log, metadata.logSize, metadata.durableSequence+1, revision.Size, content, func(commit Commit) bool {
		state.commitTurn, state.commitTime = commit.TurnID, commit.CreatedAt.UnixMilli()
		state.transactions = append(state.transactions, []any{commit.ID, commit.FirstSequence, commit.LastSequence(), commit.OperationID, commit.OperationHash, commit.TurnID, commit.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")})
		for _, event := range commit.Events {
			if event.Kind == "history/replace" {
				viewSequence = event.Sequence
			}
			digest := ""
			var contentBytes int64
			if event.PayloadRef != nil {
				digest, contentBytes = event.PayloadRef.Digest, event.PayloadRef.Bytes
				if err := insertContentRef(ctx, &state, *event.PayloadRef); err != nil {
					buildErr = err
					return false
				}
			}
			state.events = append(state.events, []any{event.Sequence, commit.ID, event.ID, event.Kind, digest, contentBytes})
			if err := indexMessageEvent(ctx, content, &state, event); err != nil {
				buildErr = err
				return false
			}
		}
		return true
	})
	if err != nil || buildErr != nil {
		return false, errors.Join(err, buildErr)
	}
	if err := validateHistoryLog(ctx, dir, log, generation); err != nil {
		return false, err
	}
	if progress.end == metadata.logSize {
		// An incomplete physical tail remains unpublished. A writer will preserve
		// and repair it before the next append.
		return true, nil
	}
	if err := flushHistoryBuildRows(ctx, tx, &state); err != nil {
		return false, err
	}
	generation = strings.TrimSuffix(generation, ":0") + fmt.Sprintf(":%d", viewSequence)
	values := map[string]string{
		"log_size": fmt.Sprint(progress.end), "log_mtime_ns": fmt.Sprint(progress.modTimeNS),
		"durable_sequence": fmt.Sprint(progress.sequence), "history_view_sequence": fmt.Sprint(viewSequence), "generation": generation,
	}
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return false, err
		}
	}
	state.statements.close()
	state.statements = nil
	if err := tx.Commit(); err != nil {
		return false, err
	}
	tx = nil
	_, _ = handle.DB.ExecContext(ctx, `PRAGMA shrink_memory`)
	return true, nil
}

func loadCurrentHistoryState(ctx context.Context, db *sql.DB) (historyBuildState, error) {
	state := historyBuildState{positions: map[string]int64{}, turns: map[string]int{}, versions: map[string]int{}}
	// Versions belong to the stable message identity, including retired rows.
	// A later rewrite can restore a removed message; its next version must not
	// collide with the versions retained for older snapshots.
	rows, err := db.QueryContext(ctx, `SELECT message_id,
		COALESCE(MAX(CASE WHEN current=1 THEN position END),0),
		COALESCE(MAX(CASE WHEN current=1 THEN visible_turn END),0),MAX(version)
		FROM messages GROUP BY message_id`)
	if err != nil {
		return historyBuildState{}, err
	}
	for rows.Next() {
		var id string
		var position int64
		var visibleTurn, version int
		if err := rows.Scan(&id, &position, &visibleTurn, &version); err != nil {
			_ = rows.Close()
			return historyBuildState{}, err
		}
		state.versions[id] = version
		if position == 0 {
			continue
		}
		state.positions[id], state.turns[id] = position, visibleTurn
		state.nextPosition = max(state.nextPosition, position)
		state.visibleTurn = max(state.visibleTurn, visibleTurn)
	}
	return state, errors.Join(rows.Err(), rows.Close())
}

func historyIndexCurrent(ctx context.Context, dir, path, sessionID string, revision logRevision) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return false
	}
	defer handle.DB.Close()
	values := map[string]string{}
	rows, err := handle.DB.QueryContext(ctx, `SELECT key,value FROM metadata WHERE key IN ('session_id','log_size','log_mtime_ns','storage_revision','projection_version','generation')`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) != nil {
			return false
		}
		values[key] = value
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return false
	}
	identity, err := readStorageIdentity(dir, manifest)
	if err != nil || !strings.HasPrefix(values["generation"], identity.Generation+":") {
		return false
	}
	return values["session_id"] == sessionID && values["log_size"] == fmt.Sprint(revision.Size) && values["log_mtime_ns"] == fmt.Sprint(revision.ModTimeNS) && values["storage_revision"] == fmt.Sprint(manifest.StorageRevision) && values["projection_version"] == fmt.Sprint(historyIndexVersion)
}

func rebuildHistoryIndex(ctx context.Context, dir, path, sessionID string, revision logRevision) error {
	manifest, err := readManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	log, err := os.Open(logPathForManifest(dir, manifest))
	if err != nil {
		return err
	}
	defer log.Close()
	content := contentStoreForSessionDir(dir)
	return projectiondb.Rebuild(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1, QuickCheck: true}, func(ctx context.Context, db *sql.DB) error {
		return populateHistoryIndex(ctx, db, log, content, dir, sessionID, revision)
	})
}

func populateHistoryIndex(ctx context.Context, db *sql.DB, log *os.File, content *sessioncontent.Store, dir, sessionID string, revision logRevision) error {
	generation, err := historyProjectionGeneration(dir, 0)
	if err != nil {
		return err
	}
	// Keep SQLite's derived-data working set explicit: even a multi-GiB history
	// must not retain its page cache in the runtime's cumulative memory.
	// Rebuild writes an unpublished, disposable replacement beside the live
	// index. Avoid WAL and durability work for that private file; Rebuild
	// validates it before one atomic publish, and the event log remains the
	// durable source if a crash leaves or corrupts the temporary database.
	if err := configureHistoryRebuild(ctx, db); err != nil {
		return err
	}
	var tx *sql.Tx
	state := historyBuildState{positions: map[string]int64{}, turns: map[string]int{}, versions: map[string]int{}}
	defer func() {
		state.statements.close()
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	beginChunk := func() error {
		var err error
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		state.tx = tx
		state.statements, err = prepareHistoryBuildStatements(ctx, tx)
		return err
	}
	if err := beginChunk(); err != nil {
		return err
	}
	var viewSequence uint64
	var buildErr error
	chunkEvents := 0
	var chunkBytes int64
	commitChunk := func() error {
		if err := flushHistoryBuildRows(ctx, tx, &state); err != nil {
			return err
		}
		state.statements.close()
		state.statements = nil
		if err := tx.Commit(); err != nil {
			return err
		}
		// modernc SQLite allocates its page cache on the Go heap. Release dirty
		// pages after each bounded transaction so a multi-GiB derived index does
		// not retain every completed chunk until the database closes.
		if _, err := db.ExecContext(ctx, `PRAGMA shrink_memory`); err != nil {
			return err
		}
		chunkEvents, chunkBytes = 0, 0
		return beginChunk()
	}
	progress, err := scanHistoryLog(ctx, log, 0, 1, revision.Size, content, func(commit Commit) bool {
		state.commitTurn, state.commitTime = commit.TurnID, commit.CreatedAt.UnixMilli()
		state.transactions = append(state.transactions, []any{commit.ID, commit.FirstSequence, commit.LastSequence(), commit.OperationID, commit.OperationHash, commit.TurnID, commit.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")})
		for _, event := range commit.Events {
			if event.Kind == "history/replace" {
				viewSequence = event.Sequence
			}
			digest := ""
			var bytes int64
			if event.PayloadRef != nil {
				digest, bytes = event.PayloadRef.Digest, event.PayloadRef.Bytes
				if err := insertContentRef(ctx, &state, *event.PayloadRef); err != nil {
					buildErr = err
					return false
				}
			}
			state.events = append(state.events, []any{event.Sequence, commit.ID, event.ID, event.Kind, digest, bytes})
			if err := indexMessageEvent(ctx, content, &state, event); err != nil {
				buildErr = err
				return false
			}
			chunkEvents++
			chunkBytes += int64(len(event.Payload))
			if event.PayloadRef != nil {
				chunkBytes += min(event.PayloadRef.Bytes, int64(historyIndexTxnBytes))
			}
			if chunkEvents >= historyIndexTxnEvents || chunkBytes >= historyIndexTxnBytes {
				if err := commitChunk(); err != nil {
					buildErr = err
					return false
				}
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	if buildErr != nil {
		return buildErr
	}
	if err := flushHistoryBuildRows(ctx, tx, &state); err != nil {
		return err
	}
	if err := validateHistoryLog(ctx, dir, log, generation); err != nil {
		return err
	}
	generation = strings.TrimSuffix(generation, ":0") + fmt.Sprintf(":%d", viewSequence)
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	metadata := map[string]string{"session_id": sessionID, "log_size": fmt.Sprint(progress.end), "log_mtime_ns": fmt.Sprint(progress.modTimeNS), "storage_revision": fmt.Sprint(manifest.StorageRevision), "projection_version": fmt.Sprint(historyIndexVersion), "durable_sequence": fmt.Sprint(progress.sequence), "history_view_sequence": fmt.Sprint(viewSequence), "generation": generation}
	for key, value := range metadata {
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key,value) VALUES(?,?)`, key, value); err != nil {
			return err
		}
	}
	state.statements.close()
	state.statements = nil
	err = tx.Commit()
	tx = nil
	return err
}

func flushHistoryBuildRows(ctx context.Context, tx *sql.Tx, state *historyBuildState) error {
	if err := insertHistoryRows(ctx, tx, `INSERT INTO transactions(commit_id,first_sequence,last_sequence,operation_id,operation_hash,turn_id,created_at) VALUES `, 7, state.transactions); err != nil {
		return err
	}
	if err := insertHistoryRows(ctx, tx, `INSERT INTO events(sequence,commit_id,event_id,kind,payload_digest,payload_bytes) VALUES `, 6, state.events); err != nil {
		return err
	}
	if err := insertHistoryRows(ctx, tx, `INSERT OR IGNORE INTO content_refs(digest,bytes,index_digest) VALUES `, 3, state.contentRefs); err != nil {
		return err
	}
	if err := insertHistoryRows(ctx, tx, `INSERT INTO messages(message_id,version,position,event_sequence,valid_to,role,preview,inline,content_digest,content_bytes,content_index_digest,current,search_text,visible_turn,visible_user) VALUES `, 15, state.messages); err != nil {
		return err
	}
	state.transactions = state.transactions[:0]
	state.events = state.events[:0]
	state.contentRefs = state.contentRefs[:0]
	state.messages = state.messages[:0]
	return nil
}

func insertHistoryRows(ctx context.Context, tx *sql.Tx, prefix string, columns int, rows [][]any) error {
	const rowsPerStatement = 128
	for start := 0; start < len(rows); start += rowsPerStatement {
		end := min(start+rowsPerStatement, len(rows))
		var query strings.Builder
		query.WriteString(prefix)
		args := make([]any, 0, (end-start)*columns)
		for rowIndex, row := range rows[start:end] {
			if len(row) != columns {
				return errors.New("session: invalid history index row width")
			}
			if rowIndex > 0 {
				query.WriteByte(',')
			}
			query.WriteByte('(')
			for column := range columns {
				if column > 0 {
					query.WriteByte(',')
				}
				query.WriteByte('?')
			}
			query.WriteByte(')')
			args = append(args, row...)
		}
		if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func indexMessageEvent(ctx context.Context, content *sessioncontent.Store, state *historyBuildState, event Event) error {
	if event.Kind == "diagnostic" {
		return indexDisplayNotice(ctx, content, state, event)
	}
	if event.Kind == "submission/accepted" {
		payload := event.Payload
		if event.PayloadRef != nil {
			var err error
			payload, err = resolveContentPayload(ctx, content, *event.PayloadRef)
			if err != nil {
				return err
			}
		}
		var receipt SubmissionReceipt
		if err := json.Unmarshal(payload, &receipt); err != nil {
			return err
		}
		_, err := state.tx.ExecContext(ctx, `INSERT INTO submissions(session_id,submission_id,message_id,sequence) VALUES(?,?,?,?) ON CONFLICT(session_id,submission_id) DO NOTHING`, receipt.SessionID, receipt.SubmissionID, receipt.MessageID, event.Sequence)
		return err
	}
	if err := indexTurnEvent(ctx, content, state, event); err != nil {
		return err
	}
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
		if err := flushHistoryBuildRows(ctx, state.tx, state); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := state.statements.expire.ExecContext(ctx, event.Sequence, id); err != nil {
				return err
			}
			delete(state.positions, id)
			delete(state.turns, id)
		}
		return renumberVisibleTurns(ctx, state, event.Sequence)
	case "message/complete", "message/upsert":
		var body struct {
			Message *provider.Message `json:"message"`
		}
		if err := strictPayload(payload, &body); err != nil || body.Message == nil {
			return damagedPayload(event, err)
		}
		message := body.Message
		if state.commitTurn != "" && message.Role == provider.RoleAssistant && !message.LocalOnly && (strings.TrimSpace(message.Content) != "" || strings.TrimSpace(message.RawContent) != "") {
			if _, err := state.tx.ExecContext(ctx, `UPDATE turn_summaries SET final_message_id=?,ended_at=MAX(ended_at,started_at+?) WHERE turn_id=?`, message.ID, message.WorkDurationMs, state.commitTurn); err != nil {
				return err
			}
		}
		return indexOneMessage(ctx, content, state, *body.Message, event.Sequence, event.Kind == "message/upsert")
	case "history/replace", "legacy/import":
		messages, err := replacementEventMessages(event, payload)
		if err != nil {
			return err
		}
		return replaceIndexedMessages(ctx, content, state, messages, event.Sequence)
	}
	return nil
}

func replaceIndexedMessages(ctx context.Context, content *sessioncontent.Store, state *historyBuildState, messages []provider.Message, sequence uint64) error {
	if err := flushHistoryBuildRows(ctx, state.tx, state); err != nil {
		return err
	}
	if _, err := state.statements.clear.ExecContext(ctx, sequence); err != nil {
		return err
	}
	state.nextPosition = 0
	state.visibleTurn = 0
	state.positions = map[string]int64{}
	state.turns = map[string]int{}
	// Keep each identity's version watermark when replacing its visible row.
	// Retired versions remain in SQLite for fixed-snapshot readers.
	legacyTurn := 0
	var final *provider.Message
	flushLegacyTurn := func() error {
		if final == nil {
			return nil
		}
		_, err := state.tx.ExecContext(ctx, `INSERT OR REPLACE INTO turn_summaries(turn_id,start_sequence,end_sequence,started_at,ended_at,final_message_id) VALUES(?,?,?,?,?,?)`, fmt.Sprintf("legacy:%d:%d", sequence, legacyTurn), sequence, sequence, 0, final.WorkDurationMs, final.ID)
		return err
	}
	for _, message := range messages {
		if message.Role == provider.RoleUser {
			if err := flushLegacyTurn(); err != nil {
				return err
			}
			legacyTurn++
			final = nil
		}
		if message.Role == provider.RoleAssistant && !message.LocalOnly && (strings.TrimSpace(message.Content) != "" || strings.TrimSpace(message.RawContent) != "") {
			copy := message
			final = &copy
		}
		if err := indexOneMessage(ctx, content, state, message, sequence, false); err != nil {
			return err
		}
	}
	return flushLegacyTurn()
}

func renumberVisibleTurns(ctx context.Context, state *historyBuildState, sequence uint64) error {
	rows, err := state.tx.QueryContext(ctx, `SELECT message_id,ordinal FROM (SELECT message_id,visible_turn,SUM(visible_user) OVER (ORDER BY position) AS ordinal FROM messages WHERE current=1) WHERE visible_turn<>ordinal`)
	if err != nil {
		return err
	}
	type changedTurn struct {
		id   string
		turn int
	}
	var changed []changedTurn
	for rows.Next() {
		var item changedTurn
		if err := rows.Scan(&item.id, &item.turn); err != nil {
			_ = rows.Close()
			return err
		}
		changed = append(changed, item)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, item := range changed {
		version := state.versions[item.id] + 1
		if _, err := state.statements.expire.ExecContext(ctx, sequence, item.id); err != nil {
			return err
		}
		if _, err := state.tx.ExecContext(ctx, `INSERT INTO messages(message_id,version,position,event_sequence,valid_to,role,preview,inline,content_digest,content_bytes,content_index_digest,current,search_text,visible_turn,visible_user) SELECT message_id,?,position,?,0,role,preview,inline,content_digest,content_bytes,content_index_digest,1,search_text,?,visible_user FROM messages WHERE message_id=? ORDER BY version DESC LIMIT 1`, version, sequence, item.turn, item.id); err != nil {
			return err
		}
		state.versions[item.id], state.turns[item.id] = version, item.turn
	}
	state.visibleTurn = 0
	for _, turn := range state.turns {
		state.visibleTurn = max(state.visibleTurn, turn)
	}
	return nil
}

func messageSearchText(message provider.Message) string {
	parts := []string{message.Content, message.RawContent, message.ReasoningContent}
	return strings.Join(parts, "\n")
}

func insertContentRef(ctx context.Context, state *historyBuildState, ref sessioncontent.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state.contentRefs = append(state.contentRefs, []any{ref.Digest, ref.Bytes, ref.IndexDigest})
	return nil
}

func messagePreview(message provider.Message) string {
	// Reference-only history rows have no origin field until hydrated. Never
	// publish host protocol text in that temporary user-message preview.
	if agent.IsHostGeneratedUserMessage(message) {
		return ""
	}
	preview := strings.TrimSpace(message.Content)
	if message.Role == provider.RoleUser {
		// Derive display text before truncating; a truncated injected block can
		// no longer be separated from the user's request.
		preview = agent.UserMessageText(message)
		if message.Origin == "" && strings.TrimSpace(message.RawContent) == "" {
			if body, ok := strings.CutPrefix(preview, `<session-context version="1">`); ok && strings.HasPrefix(strings.TrimSpace(body), "This host-generated snapshot supersedes every earlier session-context snapshot.") {
				return ""
			}
		}
	} else if preview == "" {
		preview = strings.TrimSpace(message.RawContent)
	}
	runes := []rune(preview)
	if len(runes) > 240 {
		preview = string(runes[:240])
	}
	return preview
}

func encodeHistoryCursor(cursor historyCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeHistoryCursor(value string) (historyCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return historyCursor{}, errors.New("session: invalid history cursor")
	}
	var cursor historyCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return historyCursor{}, errors.New("session: invalid history cursor")
	}
	return cursor, nil
}

func encodeSearchHistoryCursor(cursor searchHistoryCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeSearchHistoryCursor(value string) (searchHistoryCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return searchHistoryCursor{}, errors.New("session: invalid history search cursor")
	}
	var cursor searchHistoryCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return searchHistoryCursor{}, errors.New("session: invalid history search cursor")
	}
	return cursor, nil
}

func scanMetadataUint(row *sql.Row, target *uint64) error {
	var value string
	if err := row.Scan(&value); err != nil {
		return err
	}
	_, err := fmt.Sscan(value, target)
	return err
}
