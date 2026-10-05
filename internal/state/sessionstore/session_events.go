package sessionstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

const (
	sessionEventSchemaVersion = 1
	// sessionEventImageBlobsSchemaVersion stamps a record whose images live in
	// blobs. A build that predates it refuses the log instead of loading those
	// messages without their images and saving that loss back.
	sessionEventImageBlobsSchemaVersion = 3
	sessionEventTypeReplace             = "replace"
	sessionEventTypeAppend              = "append"
	// sessionEventReplayMaxBytes caps decoder input before encoding/json can
	// allocate an arbitrarily large record, keeping a corrupt log from
	// exhausting the host. It sits well above sessionEventLogFoldBytes so a log
	// an earlier build grew past that, images inline, still opens.
	sessionEventReplayMaxBytes = int64(512 << 20)
	// sessionEventLogFoldBytes bounds what a save may leave: past it, and when
	// folding would drop at least half of it again, the log is folded to one
	// replace record. Images live outside it, so a legacy inline-image log
	// shrinks to its text, while a text-only history folds only every
	// half-threshold of growth rather than on every save.
	sessionEventLogFoldBytes = int64(128 << 20)
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
	SchemaVersion int                  `json:"schema_version"`
	Type          string               `json:"type"`
	Revision      int64                `json:"revision,omitempty"`
	BaseRevision  int64                `json:"base_revision,omitempty"`
	MessageIndex  int                  `json:"message_index,omitempty"`
	Messages      []provider.Message   `json:"messages,omitempty"`
	ContentDigest string               `json:"content_digest,omitempty"`
	WriterID      string               `json:"writer_id,omitempty"`
	Reason        string               `json:"reason,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	ImageBlobs    []sessionEventImages `json:"image_blobs,omitempty"`
}

// sessionEventWireRecord keeps the messages array encoded until the replay
// budget has been checked. Decoding directly into sessionEventRecord would
// materialize every provider.Message before replay could enforce maxMessages.
type sessionEventWireRecord struct {
	SchemaVersion int                  `json:"schema_version"`
	Type          string               `json:"type"`
	Revision      int64                `json:"revision,omitempty"`
	BaseRevision  int64                `json:"base_revision,omitempty"`
	MessageIndex  int                  `json:"message_index,omitempty"`
	Messages      json.RawMessage      `json:"messages,omitempty"`
	ContentDigest string               `json:"content_digest,omitempty"`
	WriterID      string               `json:"writer_id,omitempty"`
	Reason        string               `json:"reason,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	ImageBlobs    []sessionEventImages `json:"image_blobs,omitempty"`
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

func sessionEventLogOversized(logSize, contentBytes int64, msgs []provider.Message) bool {
	if logSize > sessionEventLogFoldBytes && logSize-(contentBytes-inlineImageBytes(msgs)) > sessionEventLogFoldBytes/2 {
		return true
	}
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
	return replaySessionEventLogWithLimits(path, defaultSessionReplayLimits)
}

// nativeSessionEventSchema reports whether this build writes and replays records
// of schema v; 2 is 1.x's DAG log, read by its own replay.
func nativeSessionEventSchema(v int) bool {
	return v == sessionEventSchemaVersion || v == sessionEventImageBlobsSchemaVersion
}

// replaySessionEventLogWithLimits replays the log, then restores any image
// whose blob is gone from the copy the checkpoint still inlines.
func replaySessionEventLogWithLimits(path string, limits sessionReplayLimits) (sessionEventReplay, error) {
	replay, err := replaySessionEventRecords(path, limits)
	if err == nil {
		backfillUnavailableImages(path, replay.msgs)
	}
	return replay, err
}

func replaySessionEventRecords(path string, limits sessionReplayLimits) (sessionEventReplay, error) {
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
	limited := &io.LimitedReader{R: f, N: limits.maxBytes + 1}
	dec := json.NewDecoder(limited)
	for {
		var rec sessionEventWireRecord
		if err := dec.Decode(&rec); err != nil {
			if limited.N == 0 {
				return replay, sessionReplayLimitError(path, "encoded_bytes", limits.maxBytes+1, limits.maxBytes)
			}
			if errors.Is(err, io.EOF) {
				return replay, nil
			}
			replay.damaged = true
			return replay, nil
		}
		if !nativeSessionEventSchema(rec.SchemaVersion) {
			return replay, fmt.Errorf("decode session event log %s: unsupported schema version %d", path, rec.SchemaVersion)
		}
		if replay.records >= limits.maxRecords {
			return replay, sessionReplayLimitError(path, "event_records", int64(replay.records+1), int64(limits.maxRecords))
		}
		switch rec.Type {
		case sessionEventTypeReplace:
			msgs, collectionItems, err := decodeSessionEventMessages(path, rec.Messages, 0, 0, limits)
			if err == nil {
				collectionItems, err = resolveSessionEventImages(path, msgs, rec.ImageBlobs, collectionItems, limits)
			}
			if err != nil {
				if errors.Is(err, ErrSessionReplayLimitExceeded) {
					return replay, err
				}
				replay.damaged = true
				return replay, nil
			}
			replay.msgs = msgs
			replay.collectionItems = collectionItems
			replay.times = make([]time.Time, len(replay.msgs))
		case sessionEventTypeAppend:
			if rec.MessageIndex != len(replay.msgs) {
				replay.damaged = true
				return replay, nil
			}
			msgs, collectionItems, err := decodeSessionEventMessages(
				path, rec.Messages, len(replay.msgs), replay.collectionItems, limits,
			)
			if err == nil {
				collectionItems, err = resolveSessionEventImages(path, msgs, rec.ImageBlobs, collectionItems, limits)
			}
			if err != nil {
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
		path, trimmed, existingMessages, existingCollectionItems, limits,
	)
	if err != nil {
		return nil, existingCollectionItems, err
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, existingCollectionItems, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, existingCollectionItems, fmt.Errorf("messages must be an array")
	}
	msgs := make([]provider.Message, 0, messageCount)
	for dec.More() {
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

// resolveSessionEventImages charges a record's image references to the same
// collection budget as the messages' own arrays, then puts the images back.
func resolveSessionEventImages(
	path string, msgs []provider.Message, refs []sessionEventImages, collectionItems int, limits sessionReplayLimits,
) (int, error) {
	if len(refs) == 0 {
		return collectionItems, nil
	}
	collectionItems += len(refs) + sessionImageSlotCount(refs)
	if collectionItems > limits.maxCollectionItems {
		return collectionItems, sessionReplayLimitError(
			path, "message_collection_items", int64(collectionItems), int64(limits.maxCollectionItems),
		)
	}
	return collectionItems, resolveSessionImages(path, msgs, refs)
}

func preflightSessionEventMessages(
	path string,
	raw []byte,
	existingMessages, existingCollectionItems int,
	limits sessionReplayLimits,
) (messageCount, collectionItems int, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return 0, existingCollectionItems, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return 0, existingCollectionItems, fmt.Errorf("messages must be an array")
	}
	collectionItems = existingCollectionItems
	for dec.More() {
		if existingMessages+messageCount >= limits.maxMessages {
			return 0, existingCollectionItems, sessionReplayLimitError(
				path, "messages", int64(existingMessages+messageCount+1), int64(limits.maxMessages),
			)
		}
		messageCount++
		if err := preflightSessionEventValue(path, dec, &collectionItems, limits.maxCollectionItems); err != nil {
			return 0, existingCollectionItems, err
		}
	}
	if _, err := dec.Token(); err != nil {
		return 0, existingCollectionItems, err
	}
	return messageCount, collectionItems, nil
}

// preflightSessionEventValue walks one JSON value without materializing maps or
// slices. Each array element is charged before its value is read, so an invalid
// over-limit element cannot allocate a typed provider collection first.
func preflightSessionEventValue(path string, dec *json.Decoder, collectionItems *int, maxCollectionItems int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			if _, ok := key.(string); !ok {
				return fmt.Errorf("object key must be a string")
			}
			if err := preflightSessionEventValue(path, dec, collectionItems, maxCollectionItems); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object is not terminated")
		}
		return nil
	case '[':
		for dec.More() {
			if *collectionItems >= maxCollectionItems {
				return sessionReplayLimitError(
					path,
					"message_collection_items",
					int64(*collectionItems+1),
					int64(maxCollectionItems),
				)
			}
			(*collectionItems)++
			if err := preflightSessionEventValue(path, dec, collectionItems, maxCollectionItems); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array is not terminated")
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

// loadSessionMessages returns the session transcript, preferring the event log
// when the native layer owns it and it holds at least one decodable record.
// Foreign files squatting the log path (legacy import leftovers) are ignored
// in favor of the .jsonl checkpoint. damaged reports that a native log could
// not be replayed to its end (torn tail or corrupt record); callers that write
// should rewrite-and-compact to heal it.
func loadSessionMessages(sessionPath string) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	return loadSessionMessagesWithLimits(sessionPath, defaultSessionReplayLimits)
}

func loadSessionMessagesWithLimits(sessionPath string, limits sessionReplayLimits) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	probe, err := probeSessionEventLogWithLimits(sessionPath, limits)
	if err != nil {
		return nil, false, false, err
	}
	if probe.futureSchema || probe.dag {
		msgs, damaged, err := loadUnownedEventLog(sessionPath, probe, limits)
		return msgs, true, damaged, err
	}
	if probe.native && probe.size > 0 {
		replay, replayErr := replaySessionEventLogWithLimits(store.SessionEventLog(sessionPath), limits)
		if replayErr != nil {
			return nil, true, false, replayErr
		}
		if replay.records > 0 {
			return resolveReplayedMessages(sessionPath, replay)
		}
		// Defensive: the probe saw a native head but nothing replayed; fall
		// back to the checkpoint and let the next save rebuild the log.
		msgs, err = loadSessionMessagesFromJSONL(sessionPath)
		return msgs, false, true, err
	}
	msgs, err = loadSessionMessagesFromJSONL(sessionPath)
	return msgs, false, false, err
}

func loadSessionMessagesFromJSONL(path string) ([]provider.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var msgs []provider.Message
	dec := json.NewDecoder(f)
	for {
		var m provider.Message
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
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
	var err error
	if rec.Messages, rec.ImageBlobs, err = externalizeSessionImages(sessionPath, rec.Messages); err != nil {
		return err
	}
	fileutil.Crash("wal-append", path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rec.SchemaVersion = sessionEventRecordSchema(rec.ImageBlobs)
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
		Type:          sessionEventTypeReplace,
		Revision:      baseRevision + 1,
		BaseRevision:  baseRevision,
		Messages:      append([]provider.Message(nil), msgs...),
		ContentDigest: digestString(digest),
		WriterID:      SessionWriterID(),
		Reason:        reason,
		CreatedAt:     time.Now().UTC(),
	}
	var err error
	if rec.Messages, rec.ImageBlobs, err = externalizeSessionImages(sessionPath, rec.Messages); err != nil {
		return err
	}
	rec.SchemaVersion = sessionEventRecordSchema(rec.ImageBlobs)
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode session event: %w", err)
	}
	buf = append(buf, '\n')
	if err := fileutil.AtomicWriteFile(path, buf, 0o600); err != nil {
		return err
	}
	// The published log names every blob still needed; nothing else is.
	sweepSessionImageBlobs(sessionPath, rec.ImageBlobs)
	return nil
}

func sessionEventRecordSchema(images []sessionEventImages) int {
	if len(images) > 0 {
		return sessionEventImageBlobsSchemaVersion
	}
	return sessionEventSchemaVersion
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
	indexPath := store.SessionEventIndex(path)
	if indexPath == "" {
		return nil
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
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(indexPath), ".session-event-index.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, indexPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
