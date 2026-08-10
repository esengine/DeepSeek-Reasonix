package agent

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// SessionDisplayIndexSchemaVersion is the on-disk schema of the display index
// sidecar. Loaders reject any other version so a future layout change fails
// closed into the rescan fallback instead of misreading offsets.
const SessionDisplayIndexSchemaVersion = 1

// sessionDisplayIndexMaxLineBytes keeps recovery scans bounded when a damaged
// transcript has no newline for an arbitrarily large payload. A normal
// provider message can contain attachments, so this stays comfortably above
// the usual attachment threshold while refusing a single corrupt line before
// it can exhaust the desktop process.
const sessionDisplayIndexMaxLineBytes = 16 << 20

// SessionDisplayIndex is the per-session paging sidecar
// (<id>.display-index.json). It lets a reader page through a huge history
// without parsing whole session files: every entry carries the byte range of
// one message line plus the metadata a listing needs. Content fidelity is a
// hard constraint — the index never copies message bodies, only derived
// metadata.
//
// Offset semantics: Offset/Length describe the canonical transcript encoding —
// exactly the bytes writeSessionMessages writes for the same message slice
// (json.Marshal(msg) + '\n', with the real CreatedAt, not the zeroed form the
// content digest hashes). The .jsonl file is maintained as a random-read model
// of the authoritative event log, so published ranges are file-exact. A crash
// or an older Reasonix build may leave that model behind; consumers compare
// TranscriptSize and identity before reading and rebuild it on mismatch.
type SessionDisplayIndex struct {
	SchemaVersion  int                 `json:"schema_version"`
	Revision       int64               `json:"revision"`
	RevisionKnown  bool                `json:"revision_known"`
	ContentDigest  string              `json:"content_digest"`
	TranscriptSize int64               `json:"transcript_size"`
	MessageCount   int                 `json:"message_count"`
	AuthoredTurns  int                 `json:"authored_turns"`
	Entries        []DisplayIndexEntry `json:"entries"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

// DisplayIndexEntry is one message's metadata. AuthoredTurn is the absolute
// authored-turn number the message belongs to, counted with
// SessionPreviewFromMessages semantics (IsUserAuthoredTurn over
// UserMessageText); messages before the first authored turn carry 0.
type DisplayIndexEntry struct {
	Index        int           `json:"index"`
	Offset       int64         `json:"offset"`
	Length       int64         `json:"length"`
	Role         provider.Role `json:"role"`
	AuthoredTurn int           `json:"authored_turn"`
	StartsTurn   bool          `json:"starts_turn,omitempty"`
	HasImages    bool          `json:"has_images,omitempty"`
	HasToolCalls bool          `json:"has_tool_calls,omitempty"`
	LocalOnly    bool          `json:"local_only,omitempty"`
	ToolResult   bool          `json:"tool_result,omitempty"`
	// Synthetic and Steer classify user messages the way the desktop visible-
	// turn rule does, but on UserMessageText (the raw authored content): the
	// desktop additionally resolves @file references through a display
	// callback before testing, which a pure metadata pass cannot reproduce.
	// The predicates themselves are equivalent — control.IsSyntheticUserMessage
	// adds only a planApprovedMessage equality check that SyntheticUserPrefixes
	// already covers via its "Plan approved — plan mode is off" prefix.
	Synthetic bool `json:"synthetic,omitempty"`
	Steer     bool `json:"steer,omitempty"`
}

// BuildSessionDisplayIndex derives the index from an in-memory message slice.
// It is a pure function: same messages, same entries. It returns nil when a
// message fails to marshal — impossible for a slice that already survived
// digestAndSizeSessionMessages, so save-path callers can treat nil as a
// warn-only failure.
func BuildSessionDisplayIndex(messages []provider.Message, revision int64, revisionKnown bool, digest [sha256.Size]byte) *SessionDisplayIndex {
	idx := &SessionDisplayIndex{
		SchemaVersion: SessionDisplayIndexSchemaVersion,
		Revision:      revision,
		RevisionKnown: revisionKnown,
		ContentDigest: digestString(digest),
		MessageCount:  len(messages),
		Entries:       make([]DisplayIndexEntry, 0, len(messages)),
		UpdatedAt:     time.Now().UTC(),
	}
	if _, _, err := encodeDisplayIndexEntries(idx, messages, 0, 0, 0); err != nil {
		return nil
	}
	return idx
}

// encodeDisplayIndexEntries appends the entries for msgs[startIndex:] to idx,
// beginning at byte offset and authored-turn count turn. The encoding must
// match writeSessionMessages byte-for-byte (json.Marshal + '\n'), NOT the
// digest encoding: the digest zeroes CreatedAt while the transcript file
// stores the real bytes, and the offsets describe the file.
func encodeDisplayIndexEntries(idx *SessionDisplayIndex, msgs []provider.Message, startIndex int, offset int64, turn int) (int64, int, error) {
	for i := startIndex; i < len(msgs); i++ {
		b, err := json.Marshal(msgs[i])
		if err != nil {
			return 0, 0, fmt.Errorf("encode message %d: %w", i, err)
		}
		entry, nextTurn := classifyDisplayIndexMessage(msgs[i], i, offset, int64(len(b))+1, turn)
		turn = nextTurn
		idx.Entries = append(idx.Entries, entry)
		offset += int64(len(b)) + 1
	}
	idx.AuthoredTurns = turn
	idx.TranscriptSize = offset
	return offset, turn, nil
}

// classifyDisplayIndexMessage fills one entry's metadata from the message
// alone — no body retention, no cross-message state beyond the running turn
// counter.
func classifyDisplayIndexMessage(m provider.Message, index int, offset, length int64, turn int) (DisplayIndexEntry, int) {
	entry := DisplayIndexEntry{
		Index:        index,
		Offset:       offset,
		Length:       length,
		Role:         m.Role,
		AuthoredTurn: turn,
		HasImages:    len(m.Images) > 0,
		HasToolCalls: len(m.ToolCalls) > 0,
		LocalOnly:    m.LocalOnly,
		ToolResult:   m.Role == provider.RoleTool,
	}
	if m.Role == provider.RoleUser {
		content := UserMessageText(m)
		switch {
		case IsUserAuthoredTurn(content):
			turn++
			entry.AuthoredTurn = turn
			entry.StartsTurn = true
		default:
			if _, isSteer := SteerText(content); isSteer {
				entry.Steer = true
			} else if IsSyntheticUserText(content) {
				entry.Synthetic = true
			}
		}
	}
	return entry, turn
}

// extendSessionDisplayIndex incrementally extends the previous index when the
// save was append-only: the on-disk transcript was verified to be a prefix of
// msgs, so when the previous index describes exactly that prefix (its revision
// is the base revision this save built on and its entry count matches the
// append boundary), its entries stay valid — the canonical encoding is
// prefix-stable — and only the tail needs encoding. Any doubt returns nil and
// the caller rebuilds from the full slice.
func extendSessionDisplayIndex(indexPath string, msgs []provider.Message, digest [sha256.Size]byte, revision int64, appendFrom int) *SessionDisplayIndex {
	prev, err := LoadSessionDisplayIndex(indexPath)
	if err != nil || prev == nil {
		return nil
	}
	if prev.MessageCount != appendFrom || !prev.RevisionKnown || prev.Revision != revision-1 {
		return nil
	}
	idx := &SessionDisplayIndex{
		SchemaVersion: SessionDisplayIndexSchemaVersion,
		Revision:      revision,
		RevisionKnown: true,
		ContentDigest: digestString(digest),
		MessageCount:  len(msgs),
		Entries:       make([]DisplayIndexEntry, 0, len(msgs)),
		UpdatedAt:     time.Now().UTC(),
	}
	idx.Entries = append(idx.Entries, prev.Entries...)
	if _, _, err := encodeDisplayIndexEntries(idx, msgs, appendFrom, prev.TranscriptSize, prev.AuthoredTurns); err != nil {
		return nil
	}
	return idx
}

// refreshSessionDisplayIndex republishes the display index after a successful
// save. appendFrom >= 0 hints that the previous on-disk revision is a strict
// prefix of msgs (an append-only save), allowing an incremental extension; any
// other save shape rebuilds from the full slice. Rewind, compaction, and other
// rewrites change revision+digest, so they land here as rebuilds naturally.
func refreshSessionDisplayIndex(path string, msgs []provider.Message, digest [sha256.Size]byte, revision int64, appendFrom int) error {
	indexPath := store.SessionDisplayIndex(path)
	if indexPath == "" {
		return nil
	}
	var idx *SessionDisplayIndex
	if appendFrom > 0 {
		idx = extendSessionDisplayIndex(indexPath, msgs, digest, revision, appendFrom)
	}
	if idx == nil {
		idx = BuildSessionDisplayIndex(msgs, revision, true, digest)
	}
	if idx == nil {
		return fmt.Errorf("encode session display index")
	}
	return WriteSessionDisplayIndex(indexPath, idx)
}

// WriteSessionDisplayIndex publishes the index atomically (tmp + fsync +
// rename) with 0600 permissions, matching the other session sidecars.
func WriteSessionDisplayIndex(path string, idx *SessionDisplayIndex) error {
	if path == "" {
		return fmt.Errorf("empty session display index path")
	}
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session display index: %w", err)
	}
	b = append(b, '\n')
	if err := fileutil.AtomicWriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write session display index: %w", err)
	}
	return nil
}

// LoadSessionDisplayIndex reads the sidecar, rejecting truncated JSON,
// unsupported schema versions, and header/entry-count disagreements so a
// corrupt index fails closed into the rescan fallback.
func LoadSessionDisplayIndex(path string) (*SessionDisplayIndex, error) {
	if path == "" {
		return nil, fmt.Errorf("empty session display index path")
	}
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil, err
	}
	var idx SessionDisplayIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, fmt.Errorf("decode session display index: %w", err)
	}
	if idx.SchemaVersion != SessionDisplayIndexSchemaVersion {
		return nil, fmt.Errorf("unsupported session display index schema %d", idx.SchemaVersion)
	}
	if idx.MessageCount != len(idx.Entries) {
		return nil, fmt.Errorf("session display index message_count %d does not match %d entries", idx.MessageCount, len(idx.Entries))
	}
	expectedOffset := int64(0)
	for i, entry := range idx.Entries {
		if entry.Index != i {
			return nil, fmt.Errorf("session display index entry %d has index %d", i, entry.Index)
		}
		if entry.Offset != expectedOffset || entry.Length <= 0 || entry.Length > idx.TranscriptSize-entry.Offset {
			return nil, fmt.Errorf("session display index entry %d has invalid range %d+%d", i, entry.Offset, entry.Length)
		}
		expectedOffset += entry.Length
	}
	if expectedOffset != idx.TranscriptSize {
		return nil, fmt.Errorf("session display index covers %d bytes, transcript_size is %d", expectedOffset, idx.TranscriptSize)
	}
	return &idx, nil
}

// ValidateSessionDisplayIndex reports whether the index still describes the
// transcript identified by revision/digest/transcriptSize. The caller picks
// transcriptSize: the canonical encoding size when checking in-memory state,
// the anchor's actual file size when checking the on-disk .jsonl.
func ValidateSessionDisplayIndex(idx *SessionDisplayIndex, revision int64, revisionKnown bool, digest [sha256.Size]byte, transcriptSize int64) bool {
	if idx == nil || idx.SchemaVersion != SessionDisplayIndexSchemaVersion {
		return false
	}
	if idx.RevisionKnown != revisionKnown || (revisionKnown && idx.Revision != revision) {
		return false
	}
	if idx.ContentDigest != digestString(digest) {
		return false
	}
	if idx.TranscriptSize != transcriptSize {
		return false
	}
	return idx.MessageCount == len(idx.Entries)
}

// ScanSessionDisplayIndex is the recovery path for a missing, corrupt, or
// stale index: it streams the .jsonl transcript line by line (lines can be
// megabytes when a message carries Images, so the file is never loaded whole),
// decoding each line only as far as classification requires and recording the
// real byte offsets. The content digest is rebuilt alongside so the scanned
// index validates exactly like a built one; the revision is not recoverable
// from the transcript alone and stays unknown. A line that does not decode
// fails the scan — the caller falls back to a full parse.
func ScanSessionDisplayIndex(transcriptPath string) (*SessionDisplayIndex, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idx := &SessionDisplayIndex{
		SchemaVersion: SessionDisplayIndexSchemaVersion,
		Entries:       []DisplayIndexEntry{},
		UpdatedAt:     time.Now().UTC(),
	}
	h := sha256.New()
	reader := bufio.NewReaderSize(f, 1<<20)
	offset := int64(0)
	turn := 0
	for {
		line, readErr := readSessionDisplayIndexLine(reader)
		if len(line) > 0 {
			var m provider.Message
			if err := json.Unmarshal(line, &m); err != nil {
				return nil, fmt.Errorf("decode session transcript line %d: %w", len(idx.Entries), err)
			}
			identity, err := json.Marshal(messageForSessionIdentity(m))
			if err != nil {
				return nil, fmt.Errorf("re-encode session transcript line %d: %w", len(idx.Entries), err)
			}
			h.Write(identity)
			h.Write([]byte{'\n'})
			var entry DisplayIndexEntry
			entry, turn = classifyDisplayIndexMessage(m, len(idx.Entries), offset, int64(len(line)), turn)
			idx.Entries = append(idx.Entries, entry)
			offset += int64(len(line))
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read session transcript: %w", readErr)
		}
	}
	idx.MessageCount = len(idx.Entries)
	idx.AuthoredTurns = turn
	idx.TranscriptSize = offset
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
	idx.ContentDigest = digestString(digest)
	return idx, nil
}

// readSessionDisplayIndexLine preserves the exact byte range of one JSONL
// record while enforcing a hard per-record allocation cap. bufio.Reader's
// ReadBytes grows until it finds a delimiter, which turns a malformed giant
// line into an unbounded allocation before ScanSessionDisplayIndex can reject
// it.
func readSessionDisplayIndexLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > sessionDisplayIndexMaxLineBytes-len(line) {
			return nil, fmt.Errorf("session transcript line exceeds %d bytes", sessionDisplayIndexMaxLineBytes)
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}
