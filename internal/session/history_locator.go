package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/projectiondb"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncontent"
)

const (
	historyIndexVersion     = 10
	HistoryPageDefaultLimit = 100
	HistoryPageMaxLimit     = 500
	HistoryPageMaxBytes     = 2 << 20
	historyIndexTxnEvents   = 512
	historyIndexTxnBytes    = 8 << 20
)

// PersistentMessage is the storage/query representation of a message. It is
// deliberately separate from provider.Message: provider DTOs are materialized
// only at model or compatibility boundaries.
type PersistentMessage struct {
	ToolObservations map[string]ToolObservation `json:"toolObservations,omitempty"`
	SubmissionID     string                     `json:"submissionId,omitempty"`
	SamplingCount    *int                       `json:"samplingCount,omitempty"`
	ToolCount        *int                       `json:"toolCount,omitempty"`
	TurnFinal        bool                       `json:"turnFinal,omitempty"`
	TurnDurationMs   int64                      `json:"turnDurationMs,omitempty"`
	MessageID        string                     `json:"messageId"`
	Position         int64                      `json:"position"`
	Version          int                        `json:"version"`
	Role             string                     `json:"role"`
	Preview          string                     `json:"preview,omitempty"`
	EventSequence    uint64                     `json:"eventSequence"`
	VisibleTurn      int                        `json:"visibleTurn"`
	Inline           json.RawMessage            `json:"inline,omitempty"`
	ContentRef       *sessioncontent.Ref        `json:"contentRef,omitempty"`
}

type MessageHistoryPage struct {
	Messages         []PersistentMessage `json:"messages"`
	SnapshotSequence uint64              `json:"snapshotSequence"`
	CoverageSequence uint64              `json:"coverageSequence"`
	Status           string              `json:"status"`
	TotalTurns       int                 `json:"totalTurns"`
	Generation       string              `json:"generation"`
	NextCursor       string              `json:"nextCursor,omitempty"`
	HasMore          bool                `json:"hasMore"`
}

// MessageLocation converts a stable message identity into a cursor for a
// fixed locator snapshot. It contains no message body.
type MessageLocation struct {
	Status           string `json:"status"`
	MessageID        string `json:"messageId,omitempty"`
	SnapshotSequence uint64 `json:"snapshotSequence"`
	CoverageSequence uint64 `json:"coverageSequence"`
	Generation       string `json:"generation,omitempty"`
	Position         int64  `json:"position,omitempty"`
	VisibleTurn      int    `json:"visibleTurn,omitempty"`
	Cursor           string `json:"cursor,omitempty"`
}

// HistoryPosition is the bounded display metadata for one message in a fixed
// durable snapshot. It deliberately excludes message bodies so callers can
// plan a window without pulling the transcript into memory.
type HistoryPosition struct {
	Position    int64         `json:"position"`
	VisibleTurn int           `json:"visibleTurn"`
	Role        provider.Role `json:"role"`
}

// HistoryShape describes the complete ordering of a fixed durable snapshot
// using only small per-message metadata. Message bodies are fetched later via
// HistoryWindow.
type HistoryShape struct {
	SnapshotSequence uint64            `json:"snapshotSequence"`
	Positions        []HistoryPosition `json:"positions"`
	TotalTurns       int               `json:"totalTurns"`
}

type historyCursor struct {
	SessionID        string `json:"sessionId"`
	StorageRevision  int    `json:"storageRevision"`
	SnapshotSequence uint64 `json:"snapshotSequence"`
	BeforePosition   int64  `json:"beforePosition"`
	Projection       int    `json:"projection"`
	Generation       string `json:"generation"`
}

type historyBuildState struct {
	commitTurn   string
	commitTime   int64
	nextPosition int64
	visibleTurn  int
	positions    map[string]int64
	turns        map[string]int
	versions     map[string]int
	tx           *sql.Tx
	statements   *historyBuildStatements
	transactions [][]any
	events       [][]any
	contentRefs  [][]any
	messages     [][]any
}

type historyPreparation struct {
	done chan struct{}
	err  error
}

type historyBuildStatements struct {
	clear  *sql.Stmt
	expire *sql.Stmt
}

func prepareHistoryBuildStatements(ctx context.Context, tx *sql.Tx) (*historyBuildStatements, error) {
	statements := &historyBuildStatements{}
	queries := []struct {
		target **sql.Stmt
		query  string
	}{
		{&statements.clear, `UPDATE messages SET current=0,valid_to=? WHERE current=1`},
		{&statements.expire, `UPDATE messages SET current=0,valid_to=? WHERE message_id=? AND current=1`},
	}
	for _, candidate := range queries {
		prepared, err := tx.PrepareContext(ctx, candidate.query)
		if err != nil {
			statements.close()
			return nil, err
		}
		*candidate.target = prepared
	}
	return statements, nil
}

func (s *historyBuildStatements) close() {
	if s == nil {
		return
	}
	for _, statement := range []*sql.Stmt{s.clear, s.expire} {
		if statement != nil {
			_ = statement.Close()
		}
	}
}

func historyIndexPath(root, sessionID string) string {
	return filepath.Join(root, ".query-cache", filepath.Base(sessionID), "history-locator-v2.sqlite")
}

var historyMigrations = []projectiondb.Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`CREATE TABLE metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE transactions (commit_id TEXT PRIMARY KEY, first_sequence INTEGER NOT NULL, last_sequence INTEGER NOT NULL, operation_id TEXT NOT NULL UNIQUE, operation_hash TEXT NOT NULL, turn_id TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE events (sequence INTEGER PRIMARY KEY, commit_id TEXT NOT NULL, event_id TEXT NOT NULL UNIQUE, kind TEXT NOT NULL, payload_digest TEXT NOT NULL DEFAULT '', payload_bytes INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE messages (message_id TEXT NOT NULL, version INTEGER NOT NULL, position INTEGER NOT NULL, event_sequence INTEGER NOT NULL, valid_to INTEGER NOT NULL DEFAULT 0, role TEXT NOT NULL, preview TEXT NOT NULL, inline BLOB, content_digest TEXT NOT NULL DEFAULT '', content_bytes INTEGER NOT NULL DEFAULT 0, content_index_digest TEXT NOT NULL DEFAULT '', current INTEGER NOT NULL, PRIMARY KEY(message_id, version))`,
		`CREATE UNIQUE INDEX messages_current_position ON messages(position) WHERE current=1`,
		`CREATE INDEX messages_current_id ON messages(message_id) WHERE current=1`,
		`CREATE TABLE content_refs (digest TEXT NOT NULL, bytes INTEGER NOT NULL, index_digest TEXT NOT NULL DEFAULT '', PRIMARY KEY(digest, bytes, index_digest))`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}}, {Version: 2, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN search_text TEXT NOT NULL DEFAULT ''`)
	return err
}}, {Version: 3, Apply: func(ctx context.Context, tx *sql.Tx) error {
	// Fixed-snapshot pages walk positions newest-to-oldest. Without this index,
	// SQLite scans and sorts the full message-body table for every page; on a
	// GiB history that turns a bounded result into seconds of disk traffic.
	_, err := tx.ExecContext(ctx, `CREATE INDEX messages_snapshot_position ON messages(position DESC, event_sequence, valid_to)`)
	return err
}}, {Version: 4, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN visible_turn INTEGER NOT NULL DEFAULT 0`)
	return err
}}, {Version: 5, Apply: func(ctx context.Context, tx *sql.Tx) error {
	// Revision 5 stops duplicating inline message bodies into search_text. The
	// rebuild metadata version forces old indexes through an atomic rebuild.
	_, err := tx.ExecContext(ctx, `SELECT 1`)
	return err
}}, {Version: 6, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN visible_user INTEGER NOT NULL DEFAULT 0`)
	return err
}}, {Version: 7, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE turn_summaries (turn_id TEXT PRIMARY KEY, start_sequence INTEGER NOT NULL DEFAULT 0, end_sequence INTEGER NOT NULL DEFAULT 0, started_at INTEGER NOT NULL DEFAULT 0, ended_at INTEGER NOT NULL DEFAULT 0, final_message_id TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX turn_summaries_final ON turn_summaries(final_message_id)`)
	return err
}}, {Version: 8, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE turn_counts (turn_id TEXT NOT NULL, kind TEXT NOT NULL, id TEXT NOT NULL, sequence INTEGER NOT NULL, PRIMARY KEY(turn_id,kind,id))`)
	return err
}}, {Version: 9, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE submissions (session_id TEXT NOT NULL, submission_id TEXT NOT NULL, message_id TEXT NOT NULL, sequence INTEGER NOT NULL, PRIMARY KEY(session_id,submission_id)); CREATE INDEX submissions_message ON submissions(message_id)`)
	return err
}}, {Version: 10, Apply: func(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE tool_links (message_id TEXT NOT NULL,digest TEXT NOT NULL,call_id TEXT NOT NULL,is_result INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(message_id,digest,call_id,is_result)); CREATE INDEX tool_links_call ON tool_links(call_id,is_result); CREATE TABLE tool_states (call_id TEXT NOT NULL,sequence INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(call_id,sequence))`)
	return err
}}}

type SearchHistoryHit struct {
	MessageID     string `json:"messageId"`
	Position      int64  `json:"position"`
	Role          string `json:"role"`
	Preview       string `json:"preview"`
	EventSequence uint64 `json:"eventSequence"`
}

type SearchHistoryPage struct {
	Hits             []SearchHistoryHit `json:"hits"`
	SnapshotSequence uint64             `json:"snapshotSequence"`
	CoverageSequence uint64             `json:"coverageSequence"`
	Status           string             `json:"status"`
	NextCursor       string             `json:"nextCursor,omitempty"`
	HasMore          bool               `json:"hasMore"`
}

type searchHistoryCursor struct {
	SessionID        string `json:"sessionId"`
	StorageRevision  int    `json:"storageRevision"`
	SnapshotSequence uint64 `json:"snapshotSequence"`
	BeforePosition   int64  `json:"beforePosition"`
	Projection       int    `json:"projection"`
	QueryDigest      string `json:"queryDigest"`
	Generation       string `json:"generation"`
}

func (q *Query) HistoryPage(ctx context.Context, ref SessionRef, cursor string, limit int) (MessageHistoryPage, error) {
	if q == nil {
		return MessageHistoryPage{}, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return MessageHistoryPage{}, err
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return MessageHistoryPage{}, errors.New("session: history index requires filesystem persistence")
	}
	if limit <= 0 {
		limit = HistoryPageDefaultLimit
	}
	limit = min(limit, HistoryPageMaxLimit)
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	ready, err := q.historyLocatorReady(ctx, filesystem, ref.SessionID, path)
	if err != nil {
		return MessageHistoryPage{}, err
	}
	if !ready {
		return MessageHistoryPage{Messages: []PersistentMessage{}, Status: "preparing"}, nil
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return MessageHistoryPage{}, err
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return MessageHistoryPage{}, err
	}
	snapshot := metadata.durableSequence
	// Empty cursor means the newest page. Subsequent cursors move toward older
	// positions while the snapshot sequence remains fixed.
	before := int64(^uint64(0) >> 1)
	if cursor != "" {
		parsed, err := decodeHistoryCursor(cursor)
		if err != nil {
			return MessageHistoryPage{}, err
		}
		if parsed.SessionID != ref.SessionID || parsed.StorageRevision != metadata.storageRevision || parsed.Projection != historyIndexVersion || parsed.SnapshotSequence > snapshot || parsed.Generation != metadata.generation {
			return MessageHistoryPage{Messages: []PersistentMessage{}, Status: "stale_cursor", CoverageSequence: metadata.durableSequence, Generation: metadata.generation}, nil
		}
		snapshot = parsed.SnapshotSequence
		if parsed.BeforePosition <= 0 {
			return MessageHistoryPage{}, errors.New("session: invalid history cursor position")
		}
		before = parsed.BeforePosition
	}
	return q.readMessageHistoryPage(ctx, handle.DB, filesystem, ref, metadata, snapshot, before, limit)
}

func (q *Query) readMessageHistoryPage(ctx context.Context, db *sql.DB, filesystem *FilesystemPersistence, ref SessionRef, metadata historyIndexMetadata, snapshot uint64, before int64, limit int) (MessageHistoryPage, error) {
	page := MessageHistoryPage{Messages: []PersistentMessage{}, SnapshotSequence: snapshot, CoverageSequence: metadata.durableSequence, Status: "ready", Generation: metadata.generation}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(visible_turn),0) FROM messages WHERE event_sequence<=? AND (valid_to=0 OR valid_to>?)`, snapshot, snapshot).Scan(&page.TotalTurns); err != nil {
		return MessageHistoryPage{}, err
	}
	rows, err := db.QueryContext(ctx, `SELECT message_id,position,version,role,preview,event_sequence,visible_turn,inline,content_digest,content_bytes,content_index_digest,COALESCE((SELECT submission_id FROM submissions WHERE submissions.message_id=messages.message_id AND submissions.sequence<=messages.event_sequence AND submissions.session_id=(SELECT value FROM metadata WHERE key='session_id') LIMIT 1),'') FROM messages WHERE position<? AND event_sequence<=? AND (valid_to=0 OR valid_to>?) ORDER BY position DESC LIMIT ?`, before, snapshot, snapshot, limit+1)
	if err != nil {
		return MessageHistoryPage{}, err
	}
	defer rows.Close()
	encodedBytes := 0
	storageGeneration := q.storageGeneration(ref.SessionID)
	for rows.Next() {
		var message PersistentMessage
		var inline []byte
		var digest, indexDigest string
		var contentBytes int64
		if err := rows.Scan(&message.MessageID, &message.Position, &message.Version, &message.Role, &message.Preview, &message.EventSequence, &message.VisibleTurn, &inline, &digest, &contentBytes, &indexDigest, &message.SubmissionID); err != nil {
			return MessageHistoryPage{}, err
		}
		if len(page.Messages) == limit {
			page.HasMore = true
			break
		}
		message.Inline = append(json.RawMessage(nil), inline...)
		if digest != "" {
			contentRef := sessioncontent.Ref{Digest: digest, Bytes: contentBytes, IndexDigest: indexDigest, IntegrityBlock: sessioncontent.IntegrityBlockBytes, MediaType: "application/json"}
			if contentBytes <= recentInlineBytes && encodedBytes+int(contentBytes) <= HistoryPageMaxBytes {
				body, readErr := contentStoreForSessionDir(filepath.Join(filesystem.Root, ref.SessionID)).ReadRange(ctx, contentRef, 0, contentBytes)
				if readErr != nil {
					return MessageHistoryPage{}, readErr
				}
				message.Inline = json.RawMessage(body)
			} else {
				message.ContentRef = &contentRef
				q.authorizeContentForGeneration(ref.SessionID, storageGeneration, digest, contentBytes, indexDigest)
			}
		}
		encoded, _ := json.Marshal(message)
		if len(page.Messages) > 0 && encodedBytes+len(encoded) > HistoryPageMaxBytes {
			page.HasMore = true
			break
		}
		encodedBytes += len(encoded)
		page.Messages = append(page.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return MessageHistoryPage{}, err
	}
	if page.HasMore && len(page.Messages) > 0 {
		oldest := page.Messages[len(page.Messages)-1]
		page.NextCursor, err = encodeHistoryCursor(historyCursor{SessionID: ref.SessionID, StorageRevision: metadata.storageRevision, SnapshotSequence: snapshot, BeforePosition: oldest.Position, Projection: historyIndexVersion, Generation: metadata.generation})
		if err != nil {
			return MessageHistoryPage{}, err
		}
	}
	slices.Reverse(page.Messages)
	return page, nil
}

// LocateMessage resolves a search hit or durable message id without scanning
// message bodies. A zero snapshot selects the locator's latest covered cut.
func (q *Query) LocateMessage(ctx context.Context, ref SessionRef, messageID string, snapshot uint64) (MessageLocation, error) {
	if q == nil {
		return MessageLocation{}, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return MessageLocation{}, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return MessageLocation{}, errors.New("session: message id is required")
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return MessageLocation{}, errors.New("session: history locator requires filesystem persistence")
	}
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	ready, err := q.historyLocatorReady(ctx, filesystem, ref.SessionID, path)
	if err != nil {
		return MessageLocation{}, err
	}
	if !ready {
		return MessageLocation{Status: "preparing", MessageID: messageID}, nil
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return MessageLocation{}, err
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return MessageLocation{}, err
	}
	if snapshot == 0 {
		snapshot = metadata.durableSequence
	}
	location := MessageLocation{Status: "ready", MessageID: messageID, SnapshotSequence: snapshot, CoverageSequence: metadata.durableSequence, Generation: metadata.generation}
	if snapshot > metadata.durableSequence {
		location.Status = "preparing"
		return location, nil
	}
	err = handle.DB.QueryRowContext(ctx, `SELECT position,visible_turn FROM messages WHERE message_id=? AND event_sequence<=? AND (valid_to=0 OR valid_to>?) ORDER BY version DESC LIMIT 1`, messageID, snapshot, snapshot).Scan(&location.Position, &location.VisibleTurn)
	if errors.Is(err, sql.ErrNoRows) {
		location.Status = "not_found"
		return location, nil
	}
	if err != nil {
		return MessageLocation{}, err
	}
	location.Cursor, err = encodeHistoryCursor(historyCursor{SessionID: ref.SessionID, StorageRevision: metadata.storageRevision, SnapshotSequence: snapshot, BeforePosition: location.Position + 1, Projection: historyIndexVersion, Generation: metadata.generation})
	return location, err
}

func (q *Query) prepareHistoryLocator(filesystem *FilesystemPersistence, sessionID, path string) *historyPreparation {
	q.historyMu.Lock()
	if current := q.historyBuilds[sessionID]; current != nil {
		select {
		case <-current.done:
			if current.err != nil {
				q.historyMu.Unlock()
				return current
			}
			// A completed build may have been invalidated by a newer append.
		default:
			q.historyMu.Unlock()
			return current
		}
	}
	preparation := &historyPreparation{done: make(chan struct{})}
	q.historyBuilds[sessionID] = preparation
	q.historyMu.Unlock()
	q.rebuildMu.Lock()
	if q.closed {
		q.rebuildMu.Unlock()
		preparation.err = context.Canceled
		close(preparation.done)
		return preparation
	}
	q.rebuildWG.Add(1)
	q.rebuildMu.Unlock()
	go func() {
		defer q.rebuildWG.Done()
		// A user-requested history page or locate: highest slot priority.
		if err := q.slots.acquire(q.rebuildCtx, rebuildPriorityUser); err != nil {
			preparation.err = err
			close(preparation.done)
			return
		}
		defer q.slots.release()
		lock := q.projectionLock("history", sessionID)
		lock.Lock()
		preparation.err = ensureHistoryIndex(q.rebuildCtx, filesystem, sessionID, path)
		lock.Unlock()
		close(preparation.done)
	}()
	return preparation
}

// HistoryShape returns the ordering and visible-turn boundaries for the
// current durable snapshot without materializing any message body.
func (q *Query) HistoryShape(ctx context.Context, ref SessionRef) (HistoryShape, error) {
	filesystem, path, err := q.prepareHistoryIndex(ctx, ref)
	if err != nil {
		return HistoryShape{}, err
	}
	_ = filesystem
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return HistoryShape{}, err
	}
	defer handle.DB.Close()
	var snapshot uint64
	if err := scanMetadataUint(handle.DB.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='durable_sequence'`), &snapshot); err != nil {
		return HistoryShape{}, err
	}
	rows, err := handle.DB.QueryContext(ctx, `SELECT position,visible_turn,role FROM messages WHERE event_sequence<=? AND (valid_to=0 OR valid_to>?) ORDER BY position`, snapshot, snapshot)
	if err != nil {
		return HistoryShape{}, err
	}
	defer rows.Close()
	shape := HistoryShape{SnapshotSequence: snapshot, Positions: []HistoryPosition{}}
	for rows.Next() {
		var position HistoryPosition
		if err := rows.Scan(&position.Position, &position.VisibleTurn, &position.Role); err != nil {
			return HistoryShape{}, err
		}
		shape.Positions = append(shape.Positions, position)
		shape.TotalTurns = max(shape.TotalTurns, position.VisibleTurn)
	}
	if err := rows.Err(); err != nil {
		return HistoryShape{}, err
	}
	return shape, nil
}

// HistoryWindow materializes exactly [start,end) from a previously obtained
// durable snapshot. The snapshot must still be representable by the current
// projection; an append is allowed because version intervals retain the old
// view, while an index rebuild remains transparent.
func (q *Query) HistoryWindow(ctx context.Context, ref SessionRef, snapshot uint64, start, end int) ([]provider.Message, error) {
	filesystem, path, err := q.prepareHistoryIndex(ctx, ref)
	if err != nil {
		return nil, err
	}
	if start < 0 || end < start {
		return nil, errors.New("session: invalid history window")
	}
	handle, err := projectiondb.Open(ctx, projectiondb.OpenOptions{Path: path, Migrations: historyMigrations, RequireDisk: true, MaxOpenConns: 1})
	if err != nil {
		return nil, err
	}
	defer handle.DB.Close()
	var current uint64
	if err := scanMetadataUint(handle.DB.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='durable_sequence'`), &current); err != nil {
		return nil, err
	}
	if snapshot > current {
		return nil, errors.New("session: history snapshot is newer than durable state")
	}
	if start == end {
		return []provider.Message{}, nil
	}
	rows, err := handle.DB.QueryContext(ctx, `SELECT inline,content_digest,content_bytes,content_index_digest FROM messages WHERE position>? AND position<=? AND event_sequence<=? AND (valid_to=0 OR valid_to>?) ORDER BY position`, start, end, snapshot, snapshot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	content := contentStoreForSessionDir(filepath.Join(filesystem.Root, ref.SessionID))
	messages := make([]provider.Message, 0, end-start)
	for rows.Next() {
		var inline []byte
		var digest, indexDigest string
		var contentBytes int64
		if err := rows.Scan(&inline, &digest, &contentBytes, &indexDigest); err != nil {
			return nil, err
		}
		body := json.RawMessage(inline)
		if digest != "" {
			body, err = resolveContentPayload(ctx, content, sessioncontent.Ref{Digest: digest, Bytes: contentBytes, IndexDigest: indexDigest, IntegrityBlock: sessioncontent.IntegrityBlockBytes, MediaType: "application/json"})
			if err != nil {
				return nil, err
			}
		}
		var message provider.Message
		if err := json.Unmarshal(body, &message); err != nil {
			return nil, fmt.Errorf("session: decode indexed message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(messages) != end-start {
		return nil, fmt.Errorf("session: history window length %d, want %d", len(messages), end-start)
	}
	return messages, nil
}
