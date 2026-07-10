package agent

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// sessionEventLogCompactFloor is the smallest log size that can trigger
	// compaction, so short sessions never pay a checkpoint rewrite.
	sessionEventLogCompactFloor = int64(256 << 10)
	// sessionEventLogCompactFactor bounds the log at this multiple of the live
	// transcript's encoded size; past it the log is rewritten to one replace
	// event so replace-heavy histories (compaction, rewind) cannot grow the
	// file without bound.
	sessionEventLogCompactFactor = int64(4)
)

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

// sessionEventLogProbe classifies whatever sits at the session's event-log
// path. Legacy imports can leave a foreign ".events.jsonl" (e.g. the v0.x
// Claude-style event transcript) at exactly the native log path; writing into
// or over it would corrupt the user's original file, so foreign logs are
// read-ignored and never touched.
type sessionEventLogProbe struct {
	size          int64
	native        bool // missing/empty, or first record is a supported native event
	futureSchema  bool // first record declares a newer schema than this build
	schemaVersion int
}

// sessionEventSidecarsFit reports whether the event log and index filenames
// stay within the filesystem's name limit. Overlong transcript names (from the
// pre-bounded recovery cascade, until reconcileOverlongSessionFilenames renames
// them) must run checkpoint-only: creating their sidecars would fail with
// ENAMETOOLONG mid-save.
func sessionEventSidecarsFit(sessionPath string) bool {
	logName := filepath.Base(store.SessionEventLog(sessionPath))
	indexName := filepath.Base(store.SessionEventIndex(sessionPath))
	return len(logName) <= nameMaxBytes && len(indexName) <= nameMaxBytes
}

// probeSessionEventLog inspects the first record of the event log to decide
// whether the native persistence layer owns the file. Missing or empty logs
// count as native (we may create/append); an undecodable or foreign first
// record — or a transcript name too long for the sidecars to fit — marks the
// file as not ours.
func probeSessionEventLog(sessionPath string) (sessionEventLogProbe, error) {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return sessionEventLogProbe{native: true}, nil
	}
	if !sessionEventSidecarsFit(sessionPath) {
		return sessionEventLogProbe{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return sessionEventLogProbe{native: true}, nil
		}
		return sessionEventLogProbe{}, err
	}
	if info.IsDir() {
		return sessionEventLogProbe{}, nil
	}
	if info.Size() == 0 {
		return sessionEventLogProbe{native: true}, nil
	}
	probe := sessionEventLogProbe{size: info.Size()}
	f, err := os.Open(path)
	if err != nil {
		return sessionEventLogProbe{}, err
	}
	defer f.Close()
	var rec struct {
		SchemaVersion int    `json:"schema_version"`
		Type          string `json:"type"`
	}
	if err := json.NewDecoder(f).Decode(&rec); err != nil {
		// Nothing decodable at the head: not a native log this build can own.
		return probe, nil
	}
	probe.schemaVersion = rec.SchemaVersion
	switch {
	case rec.SchemaVersion == sessionEventSchemaVersion &&
		(rec.Type == sessionEventTypeReplace || rec.Type == sessionEventTypeAppend):
		probe.native = true
	case rec.SchemaVersion > sessionEventSchemaVersion:
		// A newer writer owns this log; ignoring or truncating it would
		// silently discard that writer's transcript.
		probe.futureSchema = true
	}
	return probe, nil
}

// replaySessionEventLog decodes an event log tolerantly: decoding stops at the
// first record that fails to parse or chain, and the state up to that point is
// returned with damaged=true so writers can self-heal. Unsupported schema
// versions and unknown event types stay hard errors — they mean a newer writer
// owns this log, and truncating it would discard that writer's data.
func replaySessionEventLog(path string) (sessionEventReplay, error) {
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
	dec := json.NewDecoder(f)
	for {
		var rec sessionEventRecord
		if err := dec.Decode(&rec); err != nil {
			if errors.Is(err, io.EOF) {
				return replay, nil
			}
			replay.damaged = true
			return replay, nil
		}
		if rec.SchemaVersion != sessionEventSchemaVersion {
			return replay, fmt.Errorf("decode session event log %s: unsupported schema version %d", path, rec.SchemaVersion)
		}
		switch rec.Type {
		case sessionEventTypeReplace:
			replay.msgs = append([]provider.Message(nil), rec.Messages...)
			replay.times = make([]time.Time, len(replay.msgs))
		case sessionEventTypeAppend:
			if rec.MessageIndex != len(replay.msgs) {
				replay.damaged = true
				return replay, nil
			}
			replay.msgs = append(replay.msgs, rec.Messages...)
			for range rec.Messages {
				replay.times = append(replay.times, rec.CreatedAt)
			}
		default:
			return replay, fmt.Errorf("decode session event log %s: unsupported event type %q", path, rec.Type)
		}
		replay.records++
		replay.lastGoodEnd = dec.InputOffset()
	}
}

// loadSessionMessages returns the session transcript, preferring the event log
// when the native layer owns it and it holds at least one decodable record.
// Foreign files squatting the log path (legacy import leftovers) are ignored
// in favor of the .jsonl checkpoint. damaged reports that a native log could
// not be replayed to its end (torn tail or corrupt record); callers that write
// should rewrite-and-compact to heal it.
func loadSessionMessages(sessionPath string) (msgs []provider.Message, fromEvents, damaged bool, err error) {
	probe, err := probeSessionEventLog(sessionPath)
	if err != nil {
		return nil, false, false, err
	}
	if probe.futureSchema {
		return nil, true, false, fmt.Errorf("session event log for %s uses schema %d; this build supports up to %d", sessionPath, probe.schemaVersion, sessionEventSchemaVersion)
	}
	if probe.native && probe.size > 0 {
		replay, replayErr := replaySessionEventLog(store.SessionEventLog(sessionPath))
		if replayErr != nil {
			return nil, true, false, replayErr
		}
		if replay.records > 0 {
			return replay.msgs, true, replay.damaged, nil
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
	if err := os.Truncate(path, replay.lastGoodEnd); err != nil {
		return err
	}
	if replay.lastGoodEnd == 0 {
		return nil
	}
	// The truncation point sits exactly at the end of a JSON value; restore
	// the trailing newline so the file stays line-oriented for external tools.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func appendSessionEvent(sessionPath string, rec sessionEventRecord, sync bool) error {
	path := store.SessionEventLog(sessionPath)
	if path == "" {
		return fmt.Errorf("empty session event log path")
	}
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
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session event log: %w", err)
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
	}, false)
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
	return fileutil.AtomicWriteFile(path, buf, 0o644)
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
			// index (e.g. after a force save folded the log away).
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
