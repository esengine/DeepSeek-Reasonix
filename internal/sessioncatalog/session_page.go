package sessioncatalog

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type sessionPageCursor struct {
	Revision uint64 `json:"r"`
	Activity int64  `json:"a"`
	Path     string `json:"p"`
}

const sessionSelectColumns = `path,directory,scope,workspace_root,topic_id,topic_title,
    custom_title,created_at,last_activity_at,preview,turns,turns_state,recovered,
    recovery_reason,recovery_digest,parent_id,content_fingerprint,meta_fingerprint,
    health,missing_since`

func scanSession(scanner interface{ Scan(...any) error }) (SessionRecord, error) {
	var record SessionRecord
	err := scanner.Scan(&record.Path, &record.Directory, &record.Scope, &record.WorkspaceRoot,
		&record.TopicID, &record.TopicTitle, &record.CustomTitle, &record.CreatedAt,
		&record.LastActivityAt, &record.Preview, &record.Turns, &record.TurnsState,
		&record.Recovered, &record.RecoveryReason, &record.RecoveryDigest,
		&record.ParentID, &record.ContentFingerprint, &record.MetaFingerprint,
		&record.Health, &record.MissingSince)
	return record, err
}

// ListSessions returns only catalog metadata. It never opens a transcript or
// sidecar and therefore remains safe on startup and UI pagination paths.
func (c *Catalog) ListSessions(ctx context.Context, req SessionPageRequest) (SessionPage, error) {
	out := SessionPage{Items: []SessionRecord{}, Revision: c.revision.Load()}
	if req.Limit <= 0 {
		req.Limit = DefaultLimit
	}
	if req.Limit > MaxLimit {
		req.Limit = MaxLimit
	}
	cursor, err := decodeSessionCursor(req.Cursor)
	if err != nil {
		return out, err
	}
	if cursor != nil && cursor.Revision != out.Revision {
		out.StaleCursor = true
		return out, nil
	}
	where := []string{`missing_since=0`, `health<>'missing'`}
	args := []any{}
	switch strings.ToLower(strings.TrimSpace(req.Scope)) {
	case "", "all":
	case "project":
		where = append(where, `scope='project'`, `workspace_root=?`)
		args = append(args, strings.TrimSpace(req.WorkspaceRoot))
	case "global":
		where = append(where, `scope='global'`)
	default:
		return out, fmt.Errorf("invalid session catalog scope %q", req.Scope)
	}
	if directory := strings.TrimSpace(req.Directory); directory != "" {
		where = append(where, `directory=?`)
		args = append(args, filepath.Clean(directory))
	}
	if query := strings.ToLower(strings.TrimSpace(req.Query)); query != "" {
		where = append(where, `(lower(custom_title) LIKE ? OR lower(preview) LIKE ? OR lower(topic_title) LIKE ? OR lower(topic_id) LIKE ?)`)
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	}
	appendSessionTimeFilter(&where, &args, req.TimeFilter, c.opts.Now())
	scanCursor := cursor
	scanLimit := max(req.Limit+1, 64)
	for len(out.Items) <= req.Limit {
		pageWhere := append([]string(nil), where...)
		pageArgs := append([]any(nil), args...)
		if scanCursor != nil {
			pageWhere = append(pageWhere, `(last_activity_at<? OR (last_activity_at=? AND path>?))`)
			pageArgs = append(pageArgs, scanCursor.Activity, scanCursor.Activity, scanCursor.Path)
		}
		pageArgs = append(pageArgs, scanLimit)
		rows, err := c.db.QueryContext(ctx, `SELECT `+sessionSelectColumns+` FROM catalog_sessions WHERE `+
			strings.Join(pageWhere, ` AND `)+` ORDER BY last_activity_at DESC,path ASC LIMIT ?`, pageArgs...)
		if err != nil {
			return out, err
		}
		rawCount := 0
		var lastScanned SessionRecord
		for rows.Next() {
			record, err := scanSession(rows)
			if err != nil {
				_ = rows.Close()
				return out, err
			}
			rawCount++
			lastScanned = record
			if c.pathRemoved(record.Path) {
				continue
			}
			out.Items = append(out.Items, record)
			if len(out.Items) > req.Limit {
				break
			}
		}
		rowsErr := rows.Err()
		_ = rows.Close()
		if rowsErr != nil {
			return out, rowsErr
		}
		if len(out.Items) > req.Limit || rawCount < scanLimit || rawCount == 0 {
			break
		}
		scanCursor = &sessionPageCursor{Activity: lastScanned.LastActivityAt, Path: lastScanned.Path}
	}
	if len(out.Items) > req.Limit {
		out.Items = out.Items[:req.Limit]
		last := out.Items[len(out.Items)-1]
		out.NextCursor = encodeSessionCursor(sessionPageCursor{Revision: out.Revision, Activity: last.LastActivityAt, Path: last.Path})
	}
	return out, nil
}

func appendSessionTimeFilter(where *[]string, args *[]any, filter string, now time.Time) {
	value := strings.ToLower(strings.TrimSpace(filter))
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch value {
	case "", "all":
	case "today":
		*where = append(*where, `last_activity_at>=?`)
		*args = append(*args, startToday.UnixMilli())
	case "yesterday":
		*where = append(*where, `last_activity_at>=?`, `last_activity_at<?`)
		*args = append(*args, startToday.AddDate(0, 0, -1).UnixMilli(), startToday.UnixMilli())
	case "older":
		*where = append(*where, `last_activity_at<?`)
		*args = append(*args, startToday.AddDate(0, 0, -1).UnixMilli())
	default:
		if cutoff := timeFilterCutoff(value, now); cutoff > 0 {
			*where = append(*where, `last_activity_at>=?`)
			*args = append(*args, cutoff)
		}
	}
}

func (c *Catalog) GetSession(ctx context.Context, path string) (SessionRecord, bool, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return SessionRecord{}, false, nil
	}
	if c.pathRemoved(path) {
		return SessionRecord{}, false, nil
	}
	record, err := scanSession(c.db.QueryRowContext(ctx, `SELECT `+sessionSelectColumns+` FROM catalog_sessions WHERE path=?`, path))
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, false, nil
	}
	return record, err == nil, err
}

func encodeSessionCursor(cursor sessionPageCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

// CursorAfter returns an exclusive pagination cursor after the given session.
func CursorAfter(revision uint64, lastActivityAt int64, path string) string {
	return encodeSessionCursor(sessionPageCursor{Revision: revision, Activity: lastActivityAt, Path: path})
}

func decodeSessionCursor(encoded string) (*sessionPageCursor, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid session catalog cursor: %w", err)
	}
	var cursor sessionPageCursor
	if err := json.Unmarshal(b, &cursor); err != nil || cursor.Path == "" {
		return nil, errors.New("invalid session catalog cursor")
	}
	return &cursor, nil
}
