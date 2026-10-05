package trustedstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"reasonix/internal/base/filelock"
)

var (
	// ErrTampered: stored bytes or a chain no longer agree with what they claim
	// or with a head this process already observed.
	ErrTampered = errors.New("trusted state tampered")
	// ErrUnwritable: the store could not persist a write. Nothing was sealed.
	ErrUnwritable = errors.New("trusted state unwritable")
	// ErrUnreadable: the store exists but could not be read.
	ErrUnreadable = errors.New("trusted state unreadable")
	// ErrNotFound: no such object, or a stream with no records yet.
	ErrNotFound = errors.New("trusted state not found")
	// ErrInvalidStream: a stream name that could escape the store's directory.
	ErrInvalidStream = errors.New("trusted state stream name invalid")
)

// FailureCode is the typed cause a caller reports for err, or "" for nil.
func FailureCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrTampered):
		return "trusted_state.tampered"
	case errors.Is(err, ErrUnwritable):
		return "trusted_state.unwritable"
	case errors.Is(err, ErrUnreadable), errors.Is(err, ErrNotFound):
		return "trusted_state.unreadable"
	case errors.Is(err, ErrInvalidStream):
		return "trusted_state.invalid_stream"
	}
	return "trusted_state.unknown"
}

// IntegrityLevel says whether a write sat inside a boundary the model cannot
// write. Only the host component that confines the model can answer it.
type IntegrityLevel string

const (
	HostProtected     IntegrityLevel = "host_protected"
	TamperEvidentOnly IntegrityLevel = "tamper_evident_only"
)

// Record is one link of a stream's chain. Payload names the object the record
// seals; Integrity is the level the record was written under.
type Record struct {
	Stream     string         `json:"stream"`
	Generation uint64         `json:"generation"`
	Parent     Digest         `json:"parent,omitempty"`
	Kind       string         `json:"kind"`
	Payload    Digest         `json:"payload"`
	Integrity  IntegrityLevel `json:"integrity"`
	At         int64          `json:"at"`
}

// Head is a stream's trusted head: its newest record and generation, and the
// integrity level the head file itself was written under.
type Head struct {
	Generation uint64         `json:"generation"`
	Record     Digest         `json:"record"`
	Integrity  IntegrityLevel `json:"integrity"`
}

// Store is one Trusted Host State root. It is safe for concurrent use, and
// processes sharing a root serialize appends per stream with a file lock.
type Store struct {
	root      string
	integrity func() IntegrityLevel
	now       func() time.Time

	mu       sync.Mutex
	observed map[string]Head
}

// Open returns the store rooted at root. integrity is asked at every write; nil
// answers TamperEvidentOnly, the level that claims nothing.
func Open(root string, integrity func() IntegrityLevel) *Store {
	if integrity == nil {
		integrity = func() IntegrityLevel { return TamperEvidentOnly }
	}
	return &Store{root: root, integrity: integrity, now: time.Now, observed: map[string]Head{}}
}

// Root is the directory the store writes under.
func (s *Store) Root() string { return s.root }

var streamName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

func (s *Store) streamDir(stream string) (string, error) {
	if !streamName.MatchString(stream) {
		return "", fmt.Errorf("%w: %q", ErrInvalidStream, stream)
	}
	return filepath.Join(s.root, "streams", stream), nil
}

// Append seals payload as the next record of stream and advances its head.
// The current head is checked against what this process observed first, so a
// rewritten history is refused rather than extended.
func (s *Store) Append(ctx context.Context, stream, kind string, payload []byte) (Record, Digest, error) {
	dir, err := s.streamDir(stream)
	if err != nil {
		return Record{}, "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Record{}, "", fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	release, err := filelock.Acquire(ctx, filepath.Join(dir, "LOCK"))
	if err != nil {
		return Record{}, "", fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	defer release()

	head, err := s.head(stream)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Record{}, "", err
	}
	payloadDigest, err := s.PutObject(payload)
	if err != nil {
		return Record{}, "", err
	}
	level := s.integrity()
	rec := Record{
		Stream:     stream,
		Generation: head.Generation + 1,
		Parent:     head.Record,
		Kind:       kind,
		Payload:    payloadDigest,
		Integrity:  level,
		At:         s.now().UnixMilli(),
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		return Record{}, "", fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	recDigest, err := s.PutObject(encoded)
	if err != nil {
		return Record{}, "", err
	}
	next := Head{Generation: rec.Generation, Record: recDigest, Integrity: level}
	headBytes, err := json.Marshal(next)
	if err != nil {
		return Record{}, "", fmt.Errorf("%w: %w", ErrUnwritable, err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "HEAD"), headBytes, true); err != nil {
		return Record{}, "", err
	}
	s.observe(stream, next)
	return rec, recDigest, nil
}

// Head returns stream's head after checking it against the head this process
// last observed. ErrNotFound means the stream has no records yet.
func (s *Store) Head(stream string) (Head, error) {
	if _, err := s.streamDir(stream); err != nil {
		return Head{}, err
	}
	return s.head(stream)
}

func (s *Store) head(stream string) (Head, error) {
	dir, err := s.streamDir(stream)
	if err != nil {
		return Head{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "HEAD"))
	if errors.Is(err, fs.ErrNotExist) {
		if prev, seen := s.observedHead(stream); seen {
			return Head{}, fmt.Errorf("%w: stream %s lost its head at generation %d", ErrTampered, stream, prev.Generation)
		}
		return Head{}, fmt.Errorf("%w: stream %s", ErrNotFound, stream)
	}
	if err != nil {
		return Head{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	var h Head
	if err := json.Unmarshal(data, &h); err != nil || h.Generation == 0 {
		return Head{}, fmt.Errorf("%w: stream %s head does not parse", ErrTampered, stream)
	}
	if err := s.consistentWithObserved(stream, h); err != nil {
		return Head{}, err
	}
	s.observe(stream, h)
	return h, nil
}

// consistentWithObserved holds a head to the one this process saw last: the
// generation may only grow, the same generation must name the same record, and
// a newer head must still reach the observed record through its parents.
func (s *Store) consistentWithObserved(stream string, h Head) error {
	prev, seen := s.observedHead(stream)
	if !seen {
		return nil
	}
	switch {
	case h.Generation < prev.Generation:
		return fmt.Errorf("%w: stream %s head went back from generation %d to %d", ErrTampered, stream, prev.Generation, h.Generation)
	case h.Generation == prev.Generation:
		if h.Record != prev.Record {
			return fmt.Errorf("%w: stream %s generation %d names a different record", ErrTampered, stream, h.Generation)
		}
		return nil
	}
	at := h.Record
	for gen := h.Generation; gen > prev.Generation; gen-- {
		rec, err := s.Record(at)
		if err != nil {
			return err
		}
		if rec.Generation != gen || rec.Stream != stream {
			return fmt.Errorf("%w: stream %s record %s is out of sequence", ErrTampered, stream, at)
		}
		at = rec.Parent
	}
	if at != prev.Record {
		return fmt.Errorf("%w: stream %s no longer reaches the observed record at generation %d", ErrTampered, stream, prev.Generation)
	}
	return nil
}

func (s *Store) observedHead(stream string) (Head, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.observed[stream]
	return h, ok
}

func (s *Store) observe(stream string, h Head) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.observed[stream]; !ok || h.Generation >= prev.Generation {
		s.observed[stream] = h
	}
}

// Record reads and checks one record object.
func (s *Store) Record(d Digest) (Record, error) {
	data, err := s.Object(d)
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("%w: record %s does not parse", ErrTampered, d)
	}
	return rec, nil
}

// Verify walks stream's whole chain from its head to the first record and
// returns the head. It proves the chain is internally whole and agrees with the
// head this process observed; it cannot prove the head itself is the true one.
func (s *Store) Verify(stream string) (Head, error) {
	h, err := s.Head(stream)
	if err != nil {
		return Head{}, err
	}
	at := h.Record
	for gen := h.Generation; gen > 0; gen-- {
		rec, err := s.Record(at)
		if err != nil {
			return Head{}, err
		}
		if rec.Generation != gen || rec.Stream != stream {
			return Head{}, fmt.Errorf("%w: stream %s record %s is out of sequence", ErrTampered, stream, at)
		}
		if _, err := s.Object(rec.Payload); err != nil {
			return Head{}, err
		}
		at = rec.Parent
	}
	if at != "" {
		return Head{}, fmt.Errorf("%w: stream %s first record has a parent", ErrTampered, stream)
	}
	return h, nil
}
