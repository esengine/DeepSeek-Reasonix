// Package v4fixture writes Reasonix 1.x sessions-v4 stores for tests, encoding
// them independently of the reader so a test checks the reader against the
// format rather than against itself.
package v4fixture

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	codec          = "reasonix.session.linear/v4"
	frameHeaderLen = 12
)

var frameMagic = [4]byte{'R', 'X', '4', 'F'}

// Ref names an object in the content pool, as 1.x writes it.
type Ref struct {
	Digest    string `json:"digest"`
	Bytes     int64  `json:"bytes"`
	MediaType string `json:"mediaType,omitempty"`
}

// Store lays down a 1.x sessions-v4 root the way 1.x writes one: a
// manifest, and events.frames as begin/event/end batches of zstd frames.
type Store struct {
	t    *testing.T
	Root string
	enc  *zstd.Encoder
	Seq  uint64
}

func New(t *testing.T) *Store {
	t.Helper()
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { enc.Close() })
	return &Store{t: t, Root: filepath.Join(t.TempDir(), "sessions-v4"), enc: enc}
}

// Event is one event of a batch: an inline payload, or one kept in the pool.
type Event struct {
	Kind    string
	Payload any
	Ref     *Ref
}

// Session creates a session directory with its manifest.
func (s *Store) Session(id string, revision int) string {
	s.t.Helper()
	dir := filepath.Join(s.Root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.t.Fatal(err)
	}
	m, _ := json.Marshal(map[string]any{
		"schemaVersion": 4, "codec": codec, "storageRevision": revision, "contentRoot": "../.content-v1",
		"sessionId": id, "createdAt": time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), "writerGeneration": 1,
	})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o600); err != nil {
		s.t.Fatal(err)
	}
	s.Seq = 1
	return dir
}

func (s *Store) frame(raw []byte) []byte {
	compressed := s.enc.EncodeAll(raw, nil)
	var header [frameHeaderLen]byte
	copy(header[:4], frameMagic[:])
	binary.BigEndian.PutUint32(header[4:8], uint32(len(compressed)))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(raw)))
	return append(header[:], compressed...)
}

// End is how a batch ends.
type End int

const (
	Ended  End = iota
	Torn       // no end record, as a crash mid-append leaves it
	BadSum     // an end record whose checksum does not match
)

// batch appends one commit.
func (s *Store) Batch(dir string, end End, events ...Event) {
	s.t.Helper()
	digest := sha256.New()
	var out []byte
	add := func(rec map[string]any, hashed bool) {
		rec["schemaVersion"], rec["codec"] = 4, codec
		raw, _ := json.Marshal(rec)
		if hashed {
			digest.Write(raw)
			digest.Write([]byte{0})
		}
		out = append(out, s.frame(raw)...)
	}
	first := s.Seq
	add(map[string]any{"recordType": "batch/begin", "commitId": "c" + hex.EncodeToString([]byte{byte(first)}),
		"operationId": "op", "operationHash": "h", "firstSeq": first, "eventCount": len(events), "writerGeneration": 1}, true)
	for i, e := range events {
		ev := map[string]any{"id": "e" + hex.EncodeToString([]byte{byte(first) + byte(i)}), "seq": first + uint64(i), "kind": e.Kind}
		if e.Ref != nil {
			ev["payloadRef"] = e.Ref
		} else {
			body, _ := json.Marshal(e.Payload)
			ev["payload"] = body
		}
		add(map[string]any{"recordType": "batch/event", "event": ev}, true)
	}
	if end != Torn {
		sum := hex.EncodeToString(digest.Sum(nil))
		if end == BadSum {
			sum = strings.Repeat("0", len(sum))
		}
		add(map[string]any{"recordType": "batch/end", "commitId": "c" + hex.EncodeToString([]byte{byte(first)}),
			"firstSeq": first, "eventCount": len(events), "sha256": sum}, false)
		s.Seq = first + uint64(len(events))
	}
	f, err := os.OpenFile(filepath.Join(dir, "events.frames"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(out); err != nil {
		s.t.Fatal(err)
	}
}

// Object puts bytes in the content pool and returns their reference.
func (s *Store) Object(data []byte, mediaType string) *Ref {
	s.t.Helper()
	sum := sha256.Sum256(data)
	d := hex.EncodeToString(sum[:])
	path := filepath.Join(s.Root, ".content-v1", "objects", d[:2], d[2:4], d)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		s.t.Fatal(err)
	}
	return &Ref{Digest: d, Bytes: int64(len(data)), MediaType: mediaType}
}

// Msg is a message event payload.
func Msg(id, role, content string, extra ...map[string]any) map[string]any {
	m := map[string]any{"id": id, "role": role, "content": content}
	for _, e := range extra {
		maps.Copy(m, e)
	}
	return map[string]any{"message": m}
}
