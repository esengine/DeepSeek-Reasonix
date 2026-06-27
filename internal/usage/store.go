// Package usage persists per-LLM-call token usage to local JSONL files so
// history, trends, and cost breakdowns survive across sessions. Each day gets
// its own file (~/.reasonix/usage/YYYY-MM-DD.jsonl); appends are atomic at the
// OS level (O_APPEND + single-write ≤ PIPE_BUF). A background goroutine
// drains a buffered channel and flushes every 5 s or every 50 records.
//
// Zero external dependencies — pure Go standard library.
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store manages per-day JSONL files in dir. It is safe for concurrent use.
type Store struct {
	dir      string
	mu       sync.Mutex
	ch       chan Record
	done     chan struct{}
	file     *os.File  // current open file handle
	fileDate string    // date of the currently open file (YYYY-MM-DD)
}

// Record is one usage event, serialised as a single JSON line.
type Record struct {
	TS               time.Time `json:"ts"`
	Provider         string    `json:"provider,omitempty"`
	Model            string    `json:"model,omitempty"`
	UsageSource      string    `json:"usage_source,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	CacheHitTokens   int       `json:"cache_hit_tokens"`
	CacheMissTokens  int       `json:"cache_miss_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	Cost             float64   `json:"cost"`
	Currency         string    `json:"currency,omitempty"`
	FinishReason     string    `json:"finish_reason,omitempty"`
	LatencyMS        int64     `json:"latency_ms,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
}

// Open creates the store directory if needed and starts the background writer.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:  dir,
		ch:   make(chan Record, 256),
		done: make(chan struct{}),
	}
	go s.writer()
	return s, nil
}

// Write submits a record to the buffered channel. Non-blocking: drops the
// record if the channel is full rather than backpressuring the caller.
func (s *Store) Write(r Record) {
	select {
	case s.ch <- r:
	default:
	}
}

// Close signals the writer goroutine to flush remaining records and exit,
// then closes the current file handle.
func (s *Store) Close() {
	close(s.ch)
	<-s.done
	s.mu.Lock()
	if s.file != nil {
		s.file.Close()
	}
	s.mu.Unlock()
}

// Dir returns the store directory path.
func (s *Store) Dir() string { return s.dir }

// Reset closes the current file handle so the next flush re-opens the file.
// Call this after deleting usage files on disk to avoid writing to a stale
// (unlinked) inode.
func (s *Store) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
	s.fileDate = ""
}

// writer runs in a background goroutine. It drains the channel, accumulates
// records into a batch, and flushes to disk every 5 seconds or when the batch
// reaches 50 records — whichever comes first.
func (s *Store) writer() {
	defer close(s.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	batch := make([]Record, 0, 50)
	for {
		select {
		case r, ok := <-s.ch:
			if !ok {
				if len(batch) > 0 {
					s.flush(batch)
				}
				return
			}
			batch = append(batch, r)
			if len(batch) >= 50 {
				s.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				s.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

// flush writes a batch of records to the appropriate day's JSONL files.
// Records are grouped by day so each file is opened at most once per batch.
func (s *Store) flush(batch []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Group by day to avoid redundant file rotations.
	groups := make(map[string][]Record)
	dayOrder := make([]string, 0, 4)
	for _, r := range batch {
		day := r.TS.Format("2006-01-02")
		if groups[day] == nil {
			dayOrder = append(dayOrder, day)
		}
		groups[day] = append(groups[day], r)
	}

	for _, day := range dayOrder {
		if err := s.rotate(day); err != nil {
			continue
		}
		for _, r := range groups[day] {
			s.writeRecord(r)
		}
	}

	if s.file != nil {
		s.file.Sync()
	}
}

// writeRecord marshals a single record and appends it as a JSON line.
func (s *Store) writeRecord(r Record) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.file.Write(append(b, '\n'))
}

// rotate opens a new file for the given date if the current handle is stale.
func (s *Store) rotate(day string) error {
	if s.fileDate == day && s.file != nil {
		return nil
	}
	if s.file != nil {
		s.file.Close()
	}
	path := filepath.Join(s.dir, day+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	s.file = f
	s.fileDate = day
	return nil
}


