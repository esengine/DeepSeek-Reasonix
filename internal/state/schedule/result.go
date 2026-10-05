package schedule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/observe"
)

const (
	// MaxReportBytes bounds the stored final report of one run.
	MaxReportBytes = 16 << 10
	// MaxResults bounds how many run results are kept; the oldest go first.
	MaxResults = 100

	maxPendingSummary = 1 << 10
	maxPendingDetail  = 4 << 10
	maxSessionPath    = 1 << 10
	resultsDirName    = "results"
)

// Result is what a finished run left for a person: its final report and what
// it parked. Every string in it is sanitized on the way in and Untrusted stays
// set on model-authored text, so a reader never has to remember to do either.
type Result struct {
	TriggerID string `json:"triggerId"`
	Report    string `json:"report"`
	// ReportUntrusted is always true: the report is model text.
	ReportUntrusted bool              `json:"reportUntrusted"`
	ReportTruncated bool              `json:"reportTruncated,omitempty"`
	Pending         []observe.Pending `json:"pending"`
	PendingDropped  int               `json:"pendingDropped,omitempty"`
	Posture         observe.Posture   `json:"posture"`
	SessionPath     string            `json:"sessionPath,omitempty"`
	RecordedAt      time.Time         `json:"recordedAt"`
}

func resultName(triggerID string) string {
	sum := sha256.Sum256([]byte(triggerID))
	return hex.EncodeToString(sum[:16]) + ".json"
}

func (s *Store) resultsDir() string { return filepath.Join(s.dir, resultsDirName) }

// PutResult records the result of a run that is still in flight. The report and
// the parked records are cleaned and cut to their bounds here, so no writer can
// store text the store's readers would have to distrust again. A run that has
// already settled (reaped while the child was slow) takes no result.
func (s *Store) PutResult(ctx context.Context, triggerID string, r Result) error {
	r = boundResult(r)
	r.TriggerID = triggerID
	r.SessionPath = s.sessionPathOrEmpty(r.SessionPath)
	return s.locked(ctx, func() error {
		m, _, err := s.loadLocked()
		if err != nil {
			return err
		}
		run := m.run(triggerID)
		if run == nil {
			return ErrRunNotFound
		}
		if !run.inFlight() {
			return ErrRunSettled
		}
		r.RecordedAt = s.clock()
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("schedule: encode result: %w", err)
		}
		if err := os.MkdirAll(s.resultsDir(), 0o700); err != nil {
			return fmt.Errorf("schedule: results dir: %w", err)
		}
		name := resultName(triggerID)
		if err := fileutil.AtomicWriteFileStrict(filepath.Join(s.resultsDir(), name), data, 0o600); err != nil {
			return fmt.Errorf("schedule: write result: %w", err)
		}
		s.pruneResults(name)
		return nil
	})
}

// GetResult returns the stored result of a run.
func (s *Store) GetResult(ctx context.Context, triggerID string) (Result, error) {
	var out Result
	err := s.locked(ctx, func() error {
		data, err := readCapped(filepath.Join(s.resultsDir(), resultName(triggerID)))
		if errors.Is(err, os.ErrNotExist) {
			return ErrResultNotFound
		}
		if err != nil {
			return fmt.Errorf("schedule: read result: %w", err)
		}
		if err := json.Unmarshal(data, &out); err != nil || out.TriggerID != triggerID {
			return ErrResultNotFound
		}
		return nil
	})
	return out, err
}

func boundResult(r Result) Result {
	r.ReportUntrusted = true
	r.Report = observe.Sanitize(r.Report)
	if len(r.Report) > MaxReportBytes {
		r.Report, r.ReportTruncated = cutUTF8(r.Report, MaxReportBytes), true
	}
	r.SessionPath = cutUTF8(observe.Sanitize(r.SessionPath), maxSessionPath)
	r.Posture.RemoteContent = false
	if len(r.Pending) > MaxResultPending {
		r.PendingDropped = len(r.Pending) - MaxResultPending
		r.Pending = r.Pending[:MaxResultPending]
	}
	kept := make([]observe.Pending, len(r.Pending))
	for i, p := range r.Pending {
		p.Summary = cutUTF8(observe.Sanitize(p.Summary), maxPendingSummary)
		p.Detail = cutUTF8(observe.Sanitize(p.Detail), maxPendingDetail)
		p.Source = cutUTF8(observe.Sanitize(p.Source), maxPendingSummary)
		if p.Detail != "" {
			p.Untrusted = true
		}
		kept[i] = p
	}
	r.Pending = kept
	return r
}

// MaxResultPending is the most parked records one stored result keeps: the most
// a run may park.
const MaxResultPending = observe.MaxPending

// ClipReport cuts a report to MaxReportBytes at a character boundary and says
// whether it cut.
func ClipReport(s string) (string, bool) {
	if len(s) <= MaxReportBytes {
		return s, false
	}
	return cutUTF8(s, MaxReportBytes), true
}

func cutUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[:limit]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func (s *Store) pruneResults(keep string) {
	entries, err := os.ReadDir(s.resultsDir())
	if err != nil || len(entries) <= MaxResults {
		return
	}
	type aged struct {
		name string
		at   time.Time
	}
	var files []aged
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == keep {
			continue
		}
		if info, err := e.Info(); err == nil {
			files = append(files, aged{e.Name(), info.ModTime()})
		}
	}
	slices.SortFunc(files, func(a, b aged) int { return a.at.Compare(b.at) })
	for _, f := range files[:max(len(files)+1-MaxResults, 0)] {
		_ = os.Remove(filepath.Join(s.resultsDir(), f.name))
	}
}

// sessionPathOrEmpty keeps a session path the child reported only when it names a
// transcript file in a sessions directory under the state root, so a reader that
// opens it cannot be pointed at credentials, trusted state or anything else.
func (s *Store) sessionPathOrEmpty(p string) string {
	if p == "" || !filepath.IsAbs(p) || filepath.Ext(p) != ".jsonl" {
		return ""
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(s.dir))
	if err != nil {
		return ""
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || filepath.Base(dir) != "sessions" {
		return ""
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return p
}
