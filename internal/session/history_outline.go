package session

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"reasonix/internal/textutil"
)

func ensureHistoryOutlineIndexes(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS messages_outline_users ON messages(visible_user,visible_turn,position,event_sequence,valid_to); CREATE INDEX IF NOT EXISTS messages_outline_answers ON messages(visible_turn,role,position DESC,event_sequence,valid_to)`); err != nil {
		return err
	}
	return tx.Commit()
}

// HistoryOutlineRequest addresses visible turns in a fixed durable cut.
type HistoryOutlineRequest struct {
	Generation       string  `json:"generation,omitempty"`
	SnapshotSequence *uint64 `json:"snapshotSequence,omitempty"`
	StartTurn        int     `json:"startTurn,omitempty"`
	Limit            int     `json:"limit,omitempty"`
}

// HistoryOutlineEntry contains display metadata, never message bodies.
type HistoryOutlineEntry struct {
	MessageID string `json:"messageId"`
	Turn      int    `json:"turn"`
	Position  int64  `json:"position"`
	Prompt    string `json:"prompt"`
	Answer    string `json:"answer,omitempty"`
}

// HistoryOutlinePage shares its identity with canonical history windows.
type HistoryOutlinePage struct {
	Status           string                `json:"status"`
	Generation       string                `json:"generation"`
	SnapshotSequence uint64                `json:"snapshotSequence"`
	CoverageSequence uint64                `json:"coverageSequence"`
	TotalTurns       int                   `json:"totalTurns"`
	Entries          []HistoryOutlineEntry `json:"entries"`
	NextTurn         int                   `json:"nextTurn"`
	Done             bool                  `json:"done"`
}

// ReadHistoryOutline reads indexed previews independently of the bounded live
// projection. The fixed version interval is identical to ReadHistoryWindow.
func (q *Query) ReadHistoryOutline(ctx context.Context, ref SessionRef, req HistoryOutlineRequest) (HistoryOutlinePage, error) {
	page := HistoryOutlinePage{Entries: []HistoryOutlineEntry{}}
	if q == nil {
		return page, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return page, err
	}
	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return page, errors.New("session: history outline requires filesystem persistence")
	}
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	ready, err := q.historyLocatorReady(ctx, filesystem, ref.SessionID, path)
	if err != nil {
		return page, err
	}
	if !ready {
		page.Status = "preparing"
		return page, nil
	}
	handle, err := openHistoryProjection(ctx, path)
	if err != nil {
		return page, err
	}
	defer handle.DB.Close()
	// These are optional accelerators, not a data-format migration. Raising
	// schema_migrations would make previous readers reject an unchanged format.
	if err := ensureHistoryOutlineIndexes(ctx, handle.DB); err != nil {
		return page, err
	}
	// Metadata and both queries belong to one SQLite read transaction. A
	// concurrent index update must not mix ordinal counts with another cut.
	tx, err := handle.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	var generation string
	var durable uint64
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT value FROM metadata WHERE key='generation'),(SELECT value FROM metadata WHERE key='durable_sequence')`).Scan(&generation, &durable); err != nil {
		return page, err
	}
	page.Generation, page.CoverageSequence, page.SnapshotSequence = generation, durable, durable
	if req.SnapshotSequence != nil {
		page.SnapshotSequence = *req.SnapshotSequence
	}
	if (req.Generation != "" && req.Generation != generation) || page.SnapshotSequence > durable {
		page.Status = "stale_cursor"
		return page, nil
	}
	cut := page.SnapshotSequence
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(visible_turn),0) FROM messages WHERE visible_user=1 AND event_sequence<=? AND (valid_to=0 OR valid_to>?)`, cut, cut).Scan(&page.TotalTurns); err != nil {
		return page, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 128
	}
	limit = min(limit, 1000)
	rows, err := tx.QueryContext(ctx, `SELECT u.message_id,u.visible_turn,u.position,u.preview,
		COALESCE((SELECT a.preview FROM messages a WHERE a.visible_turn=u.visible_turn AND a.role='assistant' AND TRIM(a.preview)<>'' AND a.event_sequence<=? AND (a.valid_to=0 OR a.valid_to>?) ORDER BY a.position DESC LIMIT 1),'')
		FROM messages u WHERE u.visible_user=1 AND u.visible_turn>=? AND u.event_sequence<=? AND (u.valid_to=0 OR u.valid_to>?) ORDER BY u.visible_turn,u.position LIMIT ?`, cut, cut, max(1, req.StartTurn), cut, cut, limit)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry HistoryOutlineEntry
		if err := rows.Scan(&entry.MessageID, &entry.Turn, &entry.Position, &entry.Prompt, &entry.Answer); err != nil {
			return page, err
		}
		entry.Prompt = textutil.ClipGraphemes(strings.Join(strings.Fields(entry.Prompt), " "), 50, "…")
		entry.Answer = textutil.ClipGraphemes(strings.Join(strings.Fields(entry.Answer), " "), 120, "…")
		page.Entries = append(page.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	page.NextTurn = max(1, req.StartTurn)
	if len(page.Entries) > 0 {
		page.NextTurn = page.Entries[len(page.Entries)-1].Turn + 1
	}
	page.Done = page.NextTurn > page.TotalTurns
	page.Status = "ready"
	return page, nil
}
