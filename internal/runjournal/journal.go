// Package runjournal persists a bounded, content-free audit trail for agent
// runs. Entries contain only classifications, counters, and SHA-256 digests;
// raw prompts, tool arguments, output, commands, and paths are never written.
package runjournal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion = 1
	maxTextBytes  = 96
	maxEntryBytes = 4096
)

// Entry is deliberately content-limited. Digests identify inputs and paths
// without persisting their values; Detail is a stable host classification.
type Entry struct {
	SchemaVersion int      `json:"schema_version"`
	Sequence      uint64   `json:"sequence"`
	Timestamp     string   `json:"timestamp"`
	Type          string   `json:"type"`
	RunID         string   `json:"run_id,omitempty"`
	ScopeDigest   string   `json:"scope_digest,omitempty"`
	Tool          string   `json:"tool,omitempty"`
	Success       *bool    `json:"success,omitempty"`
	InputDigest   string   `json:"input_digest,omitempty"`
	PathDigests   []string `json:"path_digests,omitempty"`
	OutputBytes   int      `json:"output_bytes,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
	Detail        string   `json:"detail,omitempty"`
}

// Journal is safe for concurrent receipt recording and session rebinding.
type Journal struct {
	mu   sync.Mutex
	path string
	seq  uint64
	now  func() time.Time
}

func New() *Journal { return &Journal{now: time.Now} }

// Digest returns a stable content identifier suitable for journal fields.
func Digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Bind switches the journal to path and repairs an incomplete final record.
// An empty path disables persistence. Future schema versions fail closed.
func (j *Journal) Bind(path string) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	path = strings.TrimSpace(path)
	j.path, j.seq = path, 0
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		j.path = ""
		return fmt.Errorf("read run journal: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		j.path = ""
		return fmt.Errorf("secure run journal: %w", err)
	}
	complete := b
	appendNewline := false
	if len(b) > 0 && b[len(b)-1] != '\n' {
		last := bytes.LastIndexByte(b, '\n')
		tail := b[last+1:]
		var probe Entry
		if json.Unmarshal(tail, &probe) == nil {
			appendNewline = true
		} else {
			complete = b[:last+1]
			if err := os.Truncate(path, int64(len(complete))); err != nil {
				j.path = ""
				return fmt.Errorf("repair run journal tail: %w", err)
			}
		}
	}
	s := bufio.NewScanner(bytes.NewReader(complete))
	s.Buffer(make([]byte, 4096), maxEntryBytes+1)
	for s.Scan() {
		line := s.Bytes()
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			j.path = ""
			return fmt.Errorf("invalid run journal record: %w", err)
		}
		if e.SchemaVersion > SchemaVersion {
			j.path = ""
			return fmt.Errorf("run journal schema %d is newer than supported %d", e.SchemaVersion, SchemaVersion)
		}
		if e.SchemaVersion != SchemaVersion || e.Sequence <= j.seq {
			j.path = ""
			return fmt.Errorf("invalid run journal record at sequence %d", e.Sequence)
		}
		j.seq = e.Sequence
	}
	if err := s.Err(); err != nil {
		j.path = ""
		return fmt.Errorf("scan run journal: %w", err)
	}
	if appendNewline {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = f.Write([]byte{'\n'})
		}
		if err == nil {
			err = f.Sync()
		}
		if f != nil {
			_ = f.Close()
		}
		if err != nil {
			j.path = ""
			return fmt.Errorf("repair run journal delimiter: %w", err)
		}
	}
	return nil
}

// Append validates and durably appends one record. It is a no-op while unbound.
func (j *Journal) Append(e Entry) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.path == "" {
		return nil
	}
	j.seq++
	e.SchemaVersion = SchemaVersion
	e.Sequence = j.seq
	e.Timestamp = j.now().UTC().Format(time.RFC3339Nano)
	e.Type = bounded(e.Type)
	e.RunID = bounded(e.RunID)
	e.Tool = bounded(e.Tool)
	e.Detail = bounded(e.Detail)
	e.ScopeDigest = validDigest(e.ScopeDigest)
	e.InputDigest = validDigest(e.InputDigest)
	if e.OutputBytes < 0 {
		e.OutputBytes = 0
	}
	if e.DurationMS < 0 {
		e.DurationMS = 0
	}
	if len(e.PathDigests) > 8 {
		e.PathDigests = e.PathDigests[:8]
	}
	for i := range e.PathDigests {
		e.PathDigests[i] = validDigest(e.PathDigests[i])
	}
	b, err := json.Marshal(e)
	if err != nil {
		j.seq--
		return err
	}
	if len(b)+1 > maxEntryBytes {
		j.seq--
		return fmt.Errorf("run journal record exceeds %d bytes", maxEntryBytes)
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		j.seq--
		return err
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		j.seq--
		return err
	}
	info, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		j.seq--
		return statErr
	}
	start := info.Size()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		j.seq--
		return err
	}
	_, writeErr := f.Write(append(b, '\n'))
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Truncate(j.path, start)
		j.seq--
		return writeErr
	}
	return closeErr
}

func bounded(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxTextBytes {
		return s
	}
	return s[:maxTextBytes]
}

func validDigest(s string) string {
	if len(s) == len("sha256:")+sha256.Size*2 && strings.HasPrefix(s, "sha256:") {
		if _, err := hex.DecodeString(strings.TrimPrefix(s, "sha256:")); err == nil {
			return s
		}
	}
	return ""
}
