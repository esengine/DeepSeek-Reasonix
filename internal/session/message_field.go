package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/sessioncontent"
)

// Per-field message body reading (history-window-v1 companion): a client
// requests one field of one message by stable identity — never a file path —
// and receives a bounded, UTF-8 aligned fragment with the full length and the
// next range offset. Fragments never split inside a JSON string escape
// sequence, so concatenated ranges re-parse as the original value.
const (
	// MessageFieldMaxChunk bounds one response fragment: the plan's detail
	// reading budget of at most 256 KiB per read.
	MessageFieldMaxChunk = 256 << 10
	// messageFieldMaxBody bounds the source message body a field read will
	// decode; bodies above it must go through the streaming export path.
	messageFieldMaxBody = sessioncontent.MaxReadRange
)

// MessageFieldPage is one bounded fragment of one message field.
type MessageFieldPage struct {
	Status     string `json:"status"` // ready | not_found | preparing | unsupported
	MessageID  string `json:"messageId"`
	Version    int    `json:"version"`
	Field      string `json:"field"`
	TotalBytes int64  `json:"totalBytes"`
	Offset     int64  `json:"offset"`
	Data       []byte `json:"data,omitempty"`
	NextOffset int64  `json:"nextOffset,omitempty"`
	Encoding   string `json:"encoding"`
}

// ReadMessageField returns [offset, offset+length) of one top-level field of
// the canonical message JSON. The field "canonicalMessage" addresses the
// whole stored body. Version 0 selects the message's latest version inside
// the current durable snapshot. Range credentials come from a prior window
// or page that displayed the message; a read without one is rejected.
func (q *Query) ReadMessageField(ctx context.Context, ref SessionRef, messageID string, version int, field string, offset, length int64) (MessageFieldPage, error) {
	if q == nil {
		return MessageFieldPage{}, errors.New("session: nil query")
	}
	if err := ref.validate(q.hostID); err != nil {
		return MessageFieldPage{}, err
	}
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || field == "" {
		return MessageFieldPage{}, errors.New("session: message id and field are required")
	}
	if offset < 0 {
		return MessageFieldPage{}, errors.New("session: invalid field offset")
	}
	if length <= 0 {
		length = MessageFieldMaxChunk
	}
	length = min(length, MessageFieldMaxChunk)

	filesystem, ok := q.persistence.(*FilesystemPersistence)
	if !ok {
		return MessageFieldPage{}, errors.New("session: field reads require filesystem persistence")
	}
	path := historyIndexPath(filesystem.Root, ref.SessionID)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		preparation := q.prepareHistoryLocator(filesystem, ref.SessionID, path, ctx)
		select {
		case <-preparation.done:
			if preparation.err != nil {
				return MessageFieldPage{Status: "not_found"}, preparation.err
			}
		default:
			return MessageFieldPage{Status: "preparing", MessageID: messageID, Field: field}, nil
		}
	}
	lock := q.projectionLock("history", ref.SessionID)
	lock.Lock()
	err := ensureHistoryIndex(ctx, filesystem, ref.SessionID, path)
	lock.Unlock()
	if err != nil {
		return MessageFieldPage{}, err
	}
	handle, err := openHistoryProjection(ctx, path)
	if err != nil {
		return MessageFieldPage{}, err
	}
	defer handle.DB.Close()
	metadata, err := readHistoryIndexMetadata(ctx, handle.DB)
	if err != nil {
		return MessageFieldPage{}, err
	}
	snapshot := metadata.durableSequence

	page := MessageFieldPage{Status: "ready", MessageID: messageID, Version: version, Field: field, Offset: offset, Encoding: "utf-8"}
	query := `SELECT inline,content_digest,content_bytes,content_index_digest FROM messages WHERE message_id=? AND event_sequence<=? AND (valid_to=0 OR valid_to>?)`
	args := []any{messageID, snapshot, snapshot}
	if version > 0 {
		query += ` AND version=?`
		args = append(args, version)
	}
	query += ` ORDER BY version DESC LIMIT 1`
	var inline []byte
	var digest, indexDigest string
	var contentBytes int64
	err = handle.DB.QueryRowContext(ctx, query, args...).Scan(&inline, &digest, &contentBytes, &indexDigest)
	if errors.Is(err, sql.ErrNoRows) {
		page.Status = "not_found"
		return page, nil
	}
	if err != nil {
		return MessageFieldPage{}, err
	}
	var body []byte
	if digest != "" {
		contentRef := sessioncontent.Ref{Digest: digest, Bytes: contentBytes, IndexDigest: indexDigest, IntegrityBlock: sessioncontent.IntegrityBlockBytes, MediaType: "application/json"}
		if !q.contentAuthorized(ref.SessionID, digest, contentBytes, indexDigest) {
			return MessageFieldPage{}, errors.New("session: content reference is not authorized for this session")
		}
		if contentBytes > messageFieldMaxBody {
			return MessageFieldPage{}, errors.New("session: message body exceeds the field-read budget; use the streaming export path")
		}
		body, err = contentStoreForSessionDir(filepath.Join(filesystem.Root, ref.SessionID)).ReadRange(ctx, contentRef, 0, contentBytes)
		if err != nil {
			return MessageFieldPage{}, err
		}
	} else {
		body = inline
	}

	fieldBytes, err := extractMessageField(body, field)
	if errors.Is(err, errMessageFieldAbsent) {
		// A message that simply does not carry the field is a finished empty
		// read, not a failure: optional fields like reasoning are routinely
		// absent, and a hard error would surface as a broken body.
		return page, nil
	}
	if err != nil {
		return MessageFieldPage{}, err
	}
	page.TotalBytes = int64(len(fieldBytes))
	if offset >= page.TotalBytes {
		return page, nil
	}
	end := min(offset+length, page.TotalBytes)
	// Never split a UTF-8 rune: align the fragment end back to a rune start.
	for end > offset && end < page.TotalBytes && !utf8.RuneStart(fieldBytes[end]) {
		end--
	}
	// Never split a JSON escape sequence: a cut between a backslash and the
	// character it escapes would corrupt concatenation. A cut between two
	// complete escapes inside a string is fine.
	if endsInsideEscape(fieldBytes, offset, end) {
		end--
	}
	page.Data = append([]byte(nil), fieldBytes[offset:end]...)
	page.Offset = offset
	if end < page.TotalBytes {
		page.NextOffset = end
	}
	return page, nil
}

// extractMessageField streams the canonical message JSON and captures the raw
// bytes of one top-level field value. Only the field itself is materialized —
// never the whole message.
func extractMessageField(body []byte, field string) ([]byte, error) {
	if field == "canonicalMessage" {
		return body, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	open, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := open.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("session: canonical message is not a JSON object")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("session: canonical message has a non-string key")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		if key == field {
			return raw, nil
		}
	}
	return nil, errMessageFieldAbsent
}

// errMessageFieldAbsent separates "this message has no such field" from a
// genuinely unreadable body: the caller answers the former with an empty
// finished read and the latter with an error.
var errMessageFieldAbsent = errors.New("session: message has no such field")

// endsInsideEscape reports whether a fragment ending at end would split a
// JSON escape sequence: an odd run of backslashes immediately before the cut
// means the last one dangles. begin is the fragment start, where a
// well-formed previous fragment guaranteed no dangling escape.
func endsInsideEscape(data []byte, begin, end int64) bool {
	if end <= begin {
		return false
	}
	backslashes := int64(0)
	for i := end - 1; i >= begin; i-- {
		if data[i] != '\\' {
			break
		}
		backslashes++
	}
	return backslashes%2 == 1
}
