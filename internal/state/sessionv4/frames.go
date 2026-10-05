package sessionv4

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"

	"github.com/klauspost/compress/zstd"
)

const (
	codec          = "reasonix.session.linear/v4"
	schemaVersion  = 4
	maxFrameBytes  = 8 << 20
	frameHeaderLen = 12
)

var frameMagic = [4]byte{'R', 'X', '4', 'F'}

// ErrDamaged means the log breaks its own framing or checksums.
var ErrDamaged = errors.New("1.x session log is damaged")

// ErrUnsupported means the store was written in a form this reader does not
// know, so reading on would misread it.
var ErrUnsupported = errors.New("1.x session store version is not supported")

type record struct {
	SchemaVersion int    `json:"schemaVersion"`
	Codec         string `json:"codec"`
	RecordType    string `json:"recordType"`
	CommitID      string `json:"commitId,omitempty"`
	FirstSequence uint64 `json:"firstSeq,omitempty"`
	EventCount    int    `json:"eventCount,omitempty"`
	Event         *event `json:"event,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
}

type event struct {
	ID         string          `json:"id"`
	Sequence   uint64          `json:"seq"`
	Kind       string          `json:"kind"`
	Optional   bool            `json:"optional,omitempty"`
	Payload    []byte          `json:"payload,omitempty"`
	PayloadRef *contentRef     `json:"payloadRef,omitempty"`
	resolved   json.RawMessage `json:"-"`
}

// scanCommits reads every complete batch of the log in order and hands its
// events to visit. A frame or batch cut short at the end is a write still in
// progress and ends the scan without error.
func scanCommits(store fs.FS, name string, pool contentPool, visit func([]event) error) error {
	f, err := store.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxFrameBytes))
	if err != nil {
		return err
	}
	defer dec.Close()
	var (
		pending *record
		events  []event
		digest  hash.Hash
		next    uint64 = 1
	)
	for {
		raw, complete, err := readFrame(f, dec)
		if err != nil || !complete {
			return err
		}
		var rec record
		if err := json.Unmarshal(raw, &rec); err != nil {
			return fmt.Errorf("%w: %w", ErrDamaged, err)
		}
		if rec.SchemaVersion != schemaVersion || rec.Codec != codec {
			return fmt.Errorf("%w: record codec %q", ErrUnsupported, rec.Codec)
		}
		switch rec.RecordType {
		case "batch/begin":
			if pending != nil || rec.FirstSequence != next || rec.EventCount <= 0 {
				return fmt.Errorf("%w: batch begin at sequence %d", ErrDamaged, next)
			}
			pending, events, digest = &rec, nil, sha256.New()
			digest.Write(raw)
			digest.Write([]byte{0})
		case "batch/event":
			if pending == nil || rec.Event == nil || len(events) >= pending.EventCount ||
				rec.Event.Sequence != pending.FirstSequence+uint64(len(events)) {
				return fmt.Errorf("%w: event outside its batch", ErrDamaged)
			}
			ev := *rec.Event
			if ev.resolved, err = pool.payload(ev); err != nil {
				return fmt.Errorf("%w: event %s payload: %w", ErrDamaged, ev.ID, err)
			}
			events = append(events, ev)
			digest.Write(raw)
			digest.Write([]byte{0})
		case "batch/end":
			if pending == nil || rec.CommitID != pending.CommitID || len(events) != pending.EventCount {
				return fmt.Errorf("%w: batch end", ErrDamaged)
			}
			if got := hex.EncodeToString(digest.Sum(nil)); got != rec.SHA256 {
				return fmt.Errorf("%w: batch %s checksum", ErrDamaged, pending.CommitID)
			}
			next = pending.FirstSequence + uint64(pending.EventCount)
			pending = nil
			if err := visit(events); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: record type %q", ErrUnsupported, rec.RecordType)
		}
	}
}

// readFrame reads one RX4F frame: magic, compressed length and raw length as
// big-endian uint32, then one zstd frame. complete is false at a short tail.
func readFrame(r io.Reader, dec *zstd.Decoder) ([]byte, bool, error) {
	var header [frameHeaderLen]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if [4]byte(header[:4]) != frameMagic {
		return nil, false, fmt.Errorf("%w: frame magic", ErrDamaged)
	}
	compressedLen := int(binary.BigEndian.Uint32(header[4:8]))
	rawLen := int(binary.BigEndian.Uint32(header[8:12]))
	if compressedLen <= 0 || compressedLen > maxFrameBytes || rawLen <= 0 || rawLen > maxFrameBytes {
		return nil, false, fmt.Errorf("%w: frame sizes %d/%d", ErrDamaged, compressedLen, rawLen)
	}
	compressed := make([]byte, compressedLen)
	if _, err := io.ReadFull(r, compressed); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, false, nil
		}
		return nil, false, err
	}
	raw, err := dec.DecodeAll(compressed, make([]byte, 0, rawLen))
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrDamaged, err)
	}
	if len(raw) != rawLen {
		return nil, false, fmt.Errorf("%w: frame decoded %d of %d bytes", ErrDamaged, len(raw), rawLen)
	}
	return raw, true, nil
}
