// Package stats records per-call token usage as append-only daily JSONL files
// under the user state root (config.StatsDir), and aggregates them for the
// desktop "usage statistics" panel.
//
// Design notes:
//   - Only provider usage (including request-only failures) and turn
//     completions (event.TurnDone) are recorded; turn markers power the
//     panel's "completed turns" metric and are not presented as sessions.
//   - Files are append-only JSONL; a crash mid-line leaves at most one torn
//     trailing line, which decodeRecords tolerates and skips.
package stats

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/filelock"
)

// dayLayout names one stats file per UTC-free local day, e.g. 2026-08-02.jsonl.
const dayLayout = "2006-01-02"

const appendLockTimeout = 2 * time.Second

// record is one line in a daily stats file. TurnDone marks a completed turn so
// per-day turn counts are available without touching session files.
type record struct {
	Timestamp  time.Time `json:"ts"`
	ModelRef   string    `json:"model,omitempty"`  // canonical "provider/model"
	Source     string    `json:"source,omitempty"` // desktop | cli | serve | bot | remote
	Prompt     int       `json:"prompt,omitempty"`
	Completion int       `json:"completion,omitempty"`
	Reasoning  int       `json:"reasoning,omitempty"`
	CacheHit   int       `json:"cache_hit,omitempty"`
	CacheMiss  int       `json:"cache_miss,omitempty"`
	Total      int       `json:"total,omitempty"`
	Requests   int       `json:"requests,omitempty"` // provider requests represented by this row
	Est        int       `json:"est,omitempty"`      // Prepare admission estimate; est vs prompt exposes estimate drift
	// PrefixHash fingerprints the sent request prefix (CacheDiagnostics),
	// distinguishing a miss from a changed prefix vs server-side expiry.
	PrefixHash    string   `json:"prefix_hash,omitempty"`
	PrefixChanged bool     `json:"prefix_changed,omitempty"`
	PrefixReasons []string `json:"prefix_reasons,omitempty"`
	Turn          bool     `json:"turn,omitempty"` // true for TurnDone marker rows
	// Compaction records one context-compaction pass (agent compaction
	// telemetry). Nil on usage/turn rows; Query aggregation skips them.
	Compaction *CompactionRecord `json:"compaction,omitempty"`
}

// CompactionRecord is the structured form of the agent's compaction telemetry
// detail line (trigger/mode/cache/src/proj/in/out/hit/miss/write/reqs),
// persisted so a misbehaving compaction can be diagnosed from the stats file
// alone. Error carries the full provider/estimator failure when the pass
// failed; RequestID links to provider logs when present.
type CompactionRecord struct {
	Trigger   string `json:"trigger,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Cache     string `json:"cache,omitempty"`
	SourceTok int    `json:"src,omitempty"`
	ProjTok   int    `json:"proj,omitempty"`
	InputTok  int    `json:"in,omitempty"`
	OutTok    int    `json:"out,omitempty"`
	HitTok    int    `json:"hit,omitempty"`
	MissTok   int    `json:"miss,omitempty"`
	WriteTok  int    `json:"write,omitempty"`
	Reqs      int    `json:"reqs,omitempty"`
	// Results/SavedChars describe a no-AI fast compression pass (mode=prune):
	// elided tool results, saved chars; Status records the pass outcome
	// (installed/noop/aborted/refused).
	Results    int    `json:"results,omitempty"`
	SavedChars int    `json:"saved_chars,omitempty"`
	Status     string `json:"status,omitempty"`
	// TokPerChar is the usage-calibrated token/char factor at fold time
	// (0 until a turn reports usage). It makes src/proj values verifiable:
	// src ≈ chars × tpc. Fast-compress (prune) passes leave it zero.
	TokPerChar float64 `json:"tpc,omitempty"`
	RequestID  string  `json:"provider_request_id,omitempty"`
	Error      string  `json:"err_type,omitempty"`
}

// Writer appends records to the daily stats file for a given stats dir.
type Writer struct {
	dir string
}

// NewWriter returns a Writer rooted at dir. An empty dir disables recording
// (query-only usage).
func NewWriter(dir string) *Writer {
	return &Writer{dir: strings.TrimSpace(dir)}
}

// Append writes one record, appending to the daily file (O_APPEND) so records
// from concurrent turns never overwrite each other. Each record is a single
// JSON line; a crash mid-line leaves at most one torn trailing line, which
// decodeRecords tolerates.
func (w *Writer) Append(r record) error {
	if w == nil || w.dir == "" {
		return nil
	}
	day := r.Timestamp.Format(dayLayout)
	path := filepath.Join(w.dir, day+".jsonl")
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appendLockTimeout)
	defer cancel()
	release, err := filelock.Acquire(ctx, filepath.Join(w.dir, ".append.lock"))
	if err != nil {
		return err
	}
	defer release()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ensureRecordBoundary(f); err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ensureRecordBoundary separates a torn trailing JSON object from the next
// append. The caller holds the cross-process append lock, so checking the last
// byte and repairing it cannot race another Reasonix writer.
func ensureRecordBoundary(f *os.File) error {
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return err
	}
	var tail [1]byte
	if _, err := f.ReadAt(tail[:], st.Size()-1); err != nil {
		return err
	}
	if tail[0] == '\n' {
		return nil
	}
	_, err = f.Write([]byte{'\n'})
	return err
}

// readDaily loads one daily file into records. Missing files yield nil, nil.
func readDaily(dir, day string) ([]record, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(dir, day+".jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeRecords(f)
}

// readDailyRange snapshots the available daily files with one directory scan,
// then reads only dates requested by the query. Long custom ranges are often
// mostly empty; avoiding one failed os.Open per absent day keeps their cost
// proportional to the data that actually exists.
func readDailyRange(dir string, days []string) (map[string][]record, error) {
	out := make(map[string][]record)
	if strings.TrimSpace(dir) == "" || len(days) == 0 {
		return out, nil
	}
	wanted := make(map[string]struct{}, len(days))
	for _, day := range days {
		wanted[day] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if _, ok := wanted[day]; !ok {
			continue
		}
		records, err := readDaily(dir, day)
		if err != nil {
			return nil, err
		}
		out[day] = records
	}
	return out, nil
}

func decodeRecords(r io.Reader) ([]record, error) {
	var out []record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// Malformed lines (crash mid-write or manual edit) are skipped
			// rather than failing the whole day's aggregation.
			continue
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// daysInRange lists the daily file names (without extension) whose timestamps
// intersect [from, to], inclusive.
func daysInRange(from, to time.Time) []string {
	from = dayStart(from)
	to = dayStart(to)
	if to.Before(from) {
		return nil
	}
	var days []string
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format(dayLayout))
	}
	return days
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
