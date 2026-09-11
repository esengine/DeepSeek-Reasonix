package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

const (
	sessionEventSchemaVersion = 1
	sessionEventTypeReplace   = "replace"
	sessionEventTypeAppend    = "append"
	// sessionEventReplayMaxBytes caps decoder input before encoding/json can
	// allocate an arbitrarily large record. The ceiling still accommodates
	// image-bearing histories while keeping corrupt logs from exhausting RAM.
	sessionEventReplayMaxBytes = int64(128 << 20)
	// A byte limit alone is insufficient: a compact JSON array can expand into
	// a much larger graph of messages and event records after decoding.
	sessionEventReplayMaxRecords         = 100_000
	sessionEventReplayMaxMessages        = 100_000
	sessionEventReplayMaxCollectionItems = 100_000
	sessionEventProbeMaxBytes            = int64(4 << 10)
	// sessionEventLogCompactFloor is the smallest log size that can trigger
	// event-log maintenance, so short sessions never pay a checkpoint rewrite.
	sessionEventLogCompactFloor = int64(256 << 10)
	// sessionEventLogCompactFactor bounds the log at this multiple of the live
	// transcript's encoded size; past it the log is rewritten to one replace
	// event so replace-heavy histories (rewind and recovery) cannot grow the
	// file without bound.
	sessionEventLogCompactFactor = int64(4)
)

// ErrSessionReplayLimitExceeded identifies a session that was left untouched
// because replaying it would exceed the process safety budget. Callers must not
// fall back to an older checkpoint: the event log may contain newer turns.
var ErrSessionReplayLimitExceeded = errors.New("session history exceeds safe replay limits")

// SessionReplayLimitError carries machine-readable diagnostics while keeping
// Error free of local paths for Desktop surfaces that display startup errors.
type SessionReplayLimitError struct {
	Path     string
	Resource string
	Value    int64
	Limit    int64
}

func (e *SessionReplayLimitError) Error() string {
	if e == nil {
		return ErrSessionReplayLimitExceeded.Error()
	}
	return fmt.Sprintf("%s: %s=%d, limit=%d; session files were left unchanged",
		ErrSessionReplayLimitExceeded, e.Resource, e.Value, e.Limit)
}

func (e *SessionReplayLimitError) Unwrap() error {
	return ErrSessionReplayLimitExceeded
}

type sessionReplayLimits struct {
	maxBytes           int64
	maxRecords         int
	maxMessages        int
	maxCollectionItems int
}

var defaultSessionReplayLimits = sessionReplayLimits{
	maxBytes:           sessionEventReplayMaxBytes,
	maxRecords:         sessionEventReplayMaxRecords,
	maxMessages:        sessionEventReplayMaxMessages,
	maxCollectionItems: sessionEventReplayMaxCollectionItems,
}

func sessionReplayLimitError(path, resource string, value, limit int64) error {
	err := &SessionReplayLimitError{Path: path, Resource: resource, Value: value, Limit: limit}
	slog.Warn("session: refusing unsafe event-log replay",
		"path", path, "resource", resource, "value", value, "limit", limit)
	return err
}

type sessionEventRecord struct {
	SchemaVersion int                `json:"schema_version"`
	Type          string             `json:"type"`
	Revision      int64              `json:"revision,omitempty"`
	BaseRevision  int64              `json:"base_revision,omitempty"`
	MessageIndex  int                `json:"message_index,omitempty"`
	Messages      []provider.Message `json:"messages,omitempty"`
	ContentDigest string             `json:"content_digest,omitempty"`
	WriterID      string             `json:"writer_id,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
}

// sessionEventWireRecord keeps the messages array encoded until the replay
// budget has been checked. Decoding directly into sessionEventRecord would
// materialize every provider.Message before replay could enforce maxMessages.
type sessionEventWireRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Type          string          `json:"type"`
	Revision      int64           `json:"revision,omitempty"`
	BaseRevision  int64           `json:"base_revision,omitempty"`
	MessageIndex  int             `json:"message_index,omitempty"`
	Messages      json.RawMessage `json:"messages,omitempty"`
	ContentDigest string          `json:"content_digest,omitempty"`
	WriterID      string          `json:"writer_id,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type sessionEventIndex struct {
	SchemaVersion int       `json:"schema_version"`
	LogSize       int64     `json:"log_size"`
	MessageCount  int       `json:"message_count"`
	Revision      int64     `json:"revision"`
	ContentDigest string    `json:"content_digest"`
	WriterID      string    `json:"writer_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func SessionEventLogPath(sessionPath string) string {
	return store.SessionEventLog(sessionPath)
}

func SessionEventIndexPath(sessionPath string) string {
	return store.SessionEventIndex(sessionPath)
}

func sessionEventLogSize(sessionPath string) int64 {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0
	}
	return info.Size()
}

func sessionEventLogOversized(logSize, contentBytes int64) bool {
	limit := sessionEventLogCompactFloor
	if scaled := contentBytes * sessionEventLogCompactFactor; scaled > limit {
		limit = scaled
	}
	return logSize > limit
}

// sessionEventReplay is the result of a tolerant event-log replay: the
// transcript up to the last cleanly applied record, plus enough bookkeeping
// for writers to self-heal a torn tail.
type sessionEventReplay struct {
	msgs []provider.Message
	// collectionItems counts the elements in every JSON array nested below a
	// live message. Keeping this alongside msgs bounds slices such as tool calls,
	// images, memory citations, and interrupted-turn recovery metadata without
	// coupling replay safety to today's provider.Message field list.
	collectionItems int
	// times mirrors msgs with each message's record CreatedAt. Replace events
	// collapse per-turn history, so their messages get the zero time and
	// callers fall back to coarser timestamps.
	times []time.Time
	// records counts cleanly applied events.
	records int
	// lastGoodEnd is the byte offset just past the last cleanly applied
	// record; truncating the log here drops only undecodable bytes.
	lastGoodEnd int64
	// size is the log size that was replayed.
	size int64
	// damaged is set when replay stopped early on a torn/corrupt record or a
	// broken append chain. The prefix in msgs is still a valid historical
	// state.
	damaged bool
}

// replaySessionEventLog decodes an event log tolerantly: decoding stops at the
// first record that fails to parse or chain, and the state up to that point is
// returned with damaged=true so writers can self-heal. Unsupported schema
// versions and unknown event types stay hard errors — they mean a newer writer
// owns this log, and truncating it would discard that writer's data.
func replaySessionEventLog(path string) (sessionEventReplay, error) {
	return replaySessionEventLogWithLimits(path, defaultSessionReplayLimits, nil)
}

func replaySessionEventLogWithLimits(path string, limits sessionReplayLimits, hasher *sessionTranscriptHasher) (sessionEventReplay, error) {
	return replaySessionEventLogWithContext(context.Background(), path, limits, hasher)
}

func replaySessionEventLogWithContext(ctx context.Context, path string, limits sessionReplayLimits, hasher *sessionTranscriptHasher) (sessionEventReplay, error) {
	if err := ctx.Err(); err != nil {
		return sessionEventReplay{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return sessionEventReplay{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return sessionEventReplay{}, err
	}
	replay := sessionEventReplay{size: info.Size()}
	if info.Size() > limits.maxBytes {
		return replay, sessionReplayLimitError(path, "encoded_bytes", info.Size(), limits.maxBytes)
	}
	// Stat and read are not atomic across processes. LimitReader keeps a log
	// that grows after Stat inside the same byte budget.
	limited := &io.LimitedReader{R: &contextReader{ctx: ctx, reader: f}, N: limits.maxBytes + 1}
	dec := json.NewDecoder(limited)
	for {
		if err := ctx.Err(); err != nil {
			return replay, err
		}
		var rec sessionEventWireRecord
		if err := dec.Decode(&rec); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return replay, ctxErr
			}
			if limited.N == 0 {
				return replay, sessionReplayLimitError(path, "encoded_bytes", limits.maxBytes+1, limits.maxBytes)
			}
			if errors.Is(err, io.EOF) {
				return replay, nil
			}
			replay.damaged = true
			return replay, nil
		}
		if rec.SchemaVersion != sessionEventSchemaVersion {
			return replay, fmt.Errorf("decode session event log %s: unsupported schema version %d", path, rec.SchemaVersion)
		}
		if replay.records >= limits.maxRecords {
			return replay, sessionReplayLimitError(path, "event_records", int64(replay.records+1), int64(limits.maxRecords))
		}
		switch rec.Type {
		case sessionEventTypeReplace:
			msgs, collectionItems, err := decodeSessionEventMessages(ctx, path, rec.Messages, 0, 0, limits)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return replay, ctxErr
				}
				if errors.Is(err, ErrSessionReplayLimitExceeded) {
					return replay, err
				}
				replay.damaged = true
				return replay, nil
			}
			replay.msgs = msgs
			replay.collectionItems = collectionItems
			replay.times = make([]time.Time, len(replay.msgs))
			hasher.rehash(msgs)
		case sessionEventTypeAppend:
			if rec.MessageIndex != len(replay.msgs) {
				replay.damaged = true
				return replay, nil
			}
			msgs, collectionItems, err := decodeSessionEventMessages(ctx, path, rec.Messages, len(replay.msgs), replay.collectionItems, limits)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return replay, ctxErr
				}
				if errors.Is(err, ErrSessionReplayLimitExceeded) {
					return replay, err
				}
				replay.damaged = true
				return replay, nil
			}
			replay.msgs = append(replay.msgs, msgs...)
			replay.collectionItems = collectionItems
			for range msgs {
				replay.times = append(replay.times, rec.CreatedAt)
			}
			hasher.addAll(msgs)
		default:
			return replay, fmt.Errorf("decode session event log %s: unsupported event type %q", path, rec.Type)
		}
		replay.records++
		replay.lastGoodEnd = dec.InputOffset()
	}
}

// decodeSessionEventMessages preflights both the top-level message count and
// every nested JSON collection before constructing provider.Message values.
// The token walk is independent of today's provider.Message fields, so future
// slice fields inherit the same aggregate object-graph bound automatically.
func decodeSessionEventMessages(
	ctx context.Context,
	path string,
	raw json.RawMessage,
	existingMessages, existingCollectionItems int,
	limits sessionReplayLimits,
) ([]provider.Message, int, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, existingCollectionItems, nil
	}
	messageCount, collectionItems, err := preflightSessionEventMessages(
		ctx, path, trimmed, existingMessages, existingCollectionItems, limits,
	)
	if err != nil {
		return nil, existingCollectionItems, err
	}
	dec := json.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(trimmed)})
	tok, err := dec.Token()
	if err != nil {
		return nil, existingCollectionItems, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, existingCollectionItems, fmt.Errorf("messages must be an array")
	}
	msgs := make([]provider.Message, 0, messageCount)
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return nil, existingCollectionItems, err
		}
		var msg provider.Message
		if err := dec.Decode(&msg); err != nil {
			return nil, existingCollectionItems, err
		}
		msgs = append(msgs, msg)
	}
	if _, err := dec.Token(); err != nil {
		return nil, existingCollectionItems, err
	}
	return msgs, collectionItems, nil
}

// repairSessionEventLogTail truncates undecodable bytes left by a crash or
// disk-full append so the next append cannot bury them mid-log where replay
// would stop forever. Callers must hold the session file lock. The event
// index's LogSize doubles as a cheap intact check so the common case never
// re-reads the log.
func repairSessionEventLogTail(sessionPath string) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() || info.Size() == 0 {
		return nil
	}
	if idx, err := readSessionEventIndex(sessionPath); err == nil && idx != nil && idx.LogSize == info.Size() {
		return nil
	}
	replay, err := replaySessionEventLog(path)
	if err != nil {
		return err
	}
	if replay.lastGoodEnd >= replay.size {
		return nil
	}
	// Salvage the bytes the truncation below discards. A torn tail is usually
	// one partial record, but replay also stops at a buried undecodable or
	// out-of-order record (e.g. two runtimes interleaving appends on one log) —
	// then everything past it, including intact turns, would be silently and
	// permanently lost (#6607). Preservation is best-effort: it must not block
	// the repair (the log has to become appendable again either way), and its
	// most likely failure — a full disk — is the same condition that tears
	// tails in the first place.
	if preserveErr := preserveDamagedEventLogTail(sessionPath, path, replay.lastGoodEnd, replay.size); preserveErr != nil {
		slog.Warn("session: could not preserve damaged event log tail; truncating anyway",
			"path", path, "from", replay.lastGoodEnd, "size", replay.size, "err", preserveErr)
	}
	if err := os.Truncate(path, replay.lastGoodEnd); err != nil {
		return err
	}
	if replay.lastGoodEnd == 0 {
		return nil
	}
	// The truncation point sits exactly at the end of a JSON value; restore
	// the trailing newline so the file stays line-oriented for external tools.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// preserveDamagedEventLogTail appends the about-to-be-truncated byte range of
// the event log to the .damaged salvage sidecar, prefixed with a one-line JSON
// header recording when and where the bytes came from. The sidecar is a
// forensic artifact for recovery, never replayed by the loader, and is removed
// with the session's other sidecars on delete.
func preserveDamagedEventLogTail(sessionPath, logPath string, from, to int64) error {
	if to <= from {
		return nil
	}
	src, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := src.Seek(from, io.SeekStart); err != nil {
		return err
	}
	dst, err := os.OpenFile(store.SessionEventLogDamaged(sessionPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("{\"damaged_tail\":true,\"preserved_at\":%q,\"log_offset\":%d,\"bytes\":%d}\n",
		time.Now().UTC().Format(time.RFC3339), from, to-from)
	if _, err := dst.WriteString(header); err != nil {
		dst.Close()
		return err
	}
	if _, err := io.CopyN(dst, src, to-from); err != nil && !errors.Is(err, io.EOF) {
		dst.Close()
		return err
	}
	if _, err := dst.WriteString("\n"); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func appendSessionEvent(sessionPath string, rec sessionEventRecord, sync bool) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
	fileutil.Crash("wal-append", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rec.SchemaVersion = sessionEventSchemaVersion
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if rec.WriterID == "" {
		rec.WriterID = SessionWriterID()
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode session event: %w", err)
	}
	buf = append(buf, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open session event log: %w", err)
	}
	// The event log carries the complete transcript. Chmod after opening so
	// upgrading a pre-v0.53-boundary 0644 sidecar tightens the existing inode
	// before any unredacted message is appended; OpenFile's perm only applies
	// when the file is newly created.
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("protect session event log: %w", err)
	}
	if _, err := f.Write(buf); err != nil {
		_ = f.Close()
		return fmt.Errorf("append session event: %w", err)
	}
	if sync {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

func appendSessionReplaceEvent(sessionPath string, msgs []provider.Message, digest [sha256.Size]byte, baseRevision int64, reason string) error {
	// Replace events carry the whole transcript and mark intentional history
	// rewrites; they are rare and fsynced so a power cut cannot lose one.
	return appendSessionEvent(sessionPath, sessionEventRecord{
		Type:          sessionEventTypeReplace,
		Revision:      baseRevision + 1,
		BaseRevision:  baseRevision,
		MessageIndex:  0,
		Messages:      append([]provider.Message(nil), msgs...),
		ContentDigest: digestString(digest),
		Reason:        reason,
	}, true)
}

func appendSessionAppendEvent(sessionPath string, messageIndex int, msgs []provider.Message, digest [sha256.Size]byte, baseRevision int64) error {
	if len(msgs) == 0 {
		return nil
	}
	return appendSessionEvent(sessionPath, sessionEventRecord{
		Type:          sessionEventTypeAppend,
		Revision:      baseRevision + 1,
		BaseRevision:  baseRevision,
		MessageIndex:  messageIndex,
		Messages:      append([]provider.Message(nil), msgs...),
		ContentDigest: digestString(digest),
	}, true)
}

// compactSessionEventLog rewrites the log as a single replace event via an
// atomic tmp+fsync+rename, so readers observe either the old log or the
// compacted one and never a partial state. It also heals a damaged log by
// construction.
func compactSessionEventLog(sessionPath string, msgs []provider.Message, digest [sha256.Size]byte, baseRevision int64, reason string) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
	rec := sessionEventRecord{
		SchemaVersion: sessionEventSchemaVersion,
		Type:          sessionEventTypeReplace,
		Revision:      baseRevision + 1,
		BaseRevision:  baseRevision,
		Messages:      append([]provider.Message(nil), msgs...),
		ContentDigest: digestString(digest),
		WriterID:      SessionWriterID(),
		Reason:        reason,
		CreatedAt:     time.Now().UTC(),
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode session event: %w", err)
	}
	buf = append(buf, '\n')
	return fileutil.AtomicWriteFile(path, buf, 0o600)
}

func readSessionEventIndex(sessionPath string) (*sessionEventIndex, error) {
	path := store.SessionEventIndex(sessionPath)
	if path == "" {
		return nil, nil
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil, err
	}
	var idx sessionEventIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, err
	}
	if idx.SchemaVersion != sessionEventSchemaVersion {
		return nil, fmt.Errorf("unsupported session event index schema %d", idx.SchemaVersion)
	}
	return &idx, nil
}

func writeSessionEventIndex(path string, msgs []provider.Message, digest [sha256.Size]byte, revision int64) error {
	return writeSessionEventIndexContext(context.Background(), path, msgs, digest, revision)
}

func writeSessionEventIndexContext(ctx context.Context, path string, msgs []provider.Message, digest [sha256.Size]byte, revision int64) error {
	indexPath := store.SessionEventIndex(path)
	if indexPath == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	logInfo, err := os.Stat(store.SessionEventLog(path))
	if err != nil {
		if os.IsNotExist(err) {
			// No log means nothing for the index to describe; drop a stale
			// index left by migration or manual sidecar cleanup.
			if err := os.Remove(indexPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		return err
	}
	idx := sessionEventIndex{
		SchemaVersion: sessionEventSchemaVersion,
		LogSize:       logInfo.Size(),
		MessageCount:  len(msgs),
		Revision:      revision,
		ContentDigest: digestString(digest),
		WriterID:      SessionWriterID(),
		UpdatedAt:     time.Now().UTC(),
	}
	b, err := marshalJSONIndentContext(ctx, idx)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return atomicWriteFileContext(ctx, indexPath, ".session-event-index.*.tmp", "event-index", b, 0o600, false)
}
