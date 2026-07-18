// Package sessiontelemetry owns the durable, frontend-neutral Session usage
// and read-file telemetry used by both Local and Remote runtime projections.
package sessiontelemetry

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const Version = 2

// ReadFileRecord is one successful read_file observation. Offset and Limit
// intentionally retain the existing zero-means-unspecified sidecar format.
type ReadFileRecord struct {
	Path      string `json:"path"`
	Turn      int    `json:"turn"`
	Time      int64  `json:"time"`
	Offset    int    `json:"offset,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type UsageSourceStats struct {
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	TotalTokens      int     `json:"totalTokens"`
	ReasoningTokens  int     `json:"reasoningTokens"`
	CacheHitTokens   int     `json:"cacheHitTokens"`
	CacheMissTokens  int     `json:"cacheMissTokens"`
	RequestCount     int     `json:"requestCount"`
	SessionCost      float64 `json:"sessionCost,omitempty"`
	SessionCurrency  string  `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64 `json:"sessionCostUsd,omitempty"`
}

type SourceSessionCacheCounters struct {
	Hit  int
	Miss int
}

type UsageStats struct {
	PromptTokens     int                         `json:"promptTokens"`
	CompletionTokens int                         `json:"completionTokens"`
	TotalTokens      int                         `json:"totalTokens"`
	ReasoningTokens  int                         `json:"reasoningTokens"`
	CacheHitTokens   int                         `json:"cacheHitTokens"`
	CacheMissTokens  int                         `json:"cacheMissTokens"`
	RequestCount     int                         `json:"requestCount"`
	ElapsedMs        int64                       `json:"elapsedMs"`
	SessionCost      float64                     `json:"sessionCost,omitempty"`
	SessionCurrency  string                      `json:"sessionCurrency,omitempty"`
	SessionCostUsd   float64                     `json:"sessionCostUsd,omitempty"`
	Sources          map[string]UsageSourceStats `json:"sources,omitempty"`

	// Runtime-only accounting is intentionally absent from the sidecar. A
	// restarted process cannot truthfully reconstruct elapsed time for a turn
	// that was interrupted between persisted events.
	ActiveTurnStartedAt int64                                 `json:"-"`
	SourceSessionCache  map[string]SourceSessionCacheCounters `json:"-"`
}

type Snapshot struct {
	Version   int              `json:"version"`
	ReadFiles []ReadFileRecord `json:"readFiles"`
	Usage     UsageStats       `json:"usage"`
}

// Clone returns an ownership-independent copy, including runtime-only cache
// counters when the snapshot is used to transfer a live Local runtime.
func (s Snapshot) Clone() Snapshot {
	out := s
	out.ReadFiles = append([]ReadFileRecord(nil), s.ReadFiles...)
	out.Usage = s.Usage.Clone()
	if out.ReadFiles == nil {
		out.ReadFiles = []ReadFileRecord{}
	}
	return out
}

func (s UsageStats) Clone() UsageStats {
	out := s
	if len(s.Sources) > 0 {
		out.Sources = make(map[string]UsageSourceStats, len(s.Sources))
		for source, stats := range s.Sources {
			out.Sources[source] = stats
		}
	}
	if len(s.SourceSessionCache) > 0 {
		out.SourceSessionCache = make(map[string]SourceSessionCacheCounters, len(s.SourceSessionCache))
		for source, counters := range s.SourceSessionCache {
			out.SourceSessionCache[source] = counters
		}
	}
	return out
}

// TurnStarted starts elapsed-time accounting once. Duplicate deliveries do not
// reset the clock.
func (s *UsageStats) TurnStarted(nowMs int64) {
	if s.ActiveTurnStartedAt == 0 {
		s.ActiveTurnStartedAt = nowMs
	}
}

func (s *UsageStats) TurnDone(nowMs int64) {
	if started := s.ActiveTurnStartedAt; started > 0 && nowMs >= started {
		s.ElapsedMs += nowMs - started
	}
	s.ActiveTurnStartedAt = 0
}

// RecordUsage applies the existing Local telemetry rules to the shared wire
// event. Source defaults to executor; executor/planner session-cache counters
// are converted from cumulative values to per-event deltas.
func (s *UsageStats) RecordUsage(usage *eventwire.Usage) {
	if usage == nil {
		return
	}
	source := strings.TrimSpace(usage.Source)
	if source == "" {
		source = event.UsageSourceExecutor
	}
	s.PromptTokens += usage.PromptTokens
	s.CompletionTokens += usage.CompletionTokens
	s.TotalTokens += usage.TotalTokens
	s.ReasoningTokens += usage.ReasoningTokens
	hit, miss := s.cacheTokenDelta(source, usage)
	s.CacheHitTokens += hit
	s.CacheMissTokens += miss
	s.RequestCount++
	if s.Sources == nil {
		s.Sources = map[string]UsageSourceStats{}
	}
	sourceStats := s.Sources[source]
	sourceStats.PromptTokens += usage.PromptTokens
	sourceStats.CompletionTokens += usage.CompletionTokens
	sourceStats.TotalTokens += usage.TotalTokens
	sourceStats.ReasoningTokens += usage.ReasoningTokens
	sourceStats.CacheHitTokens += hit
	sourceStats.CacheMissTokens += miss
	sourceStats.RequestCount++
	cost := usage.Cost
	if cost == 0 {
		cost = usage.CostUSD
	}
	if cost != 0 || usage.Currency != "" {
		s.SessionCost += cost
		s.SessionCostUsd = s.SessionCost
		s.SessionCurrency = usage.Currency
		sourceStats.SessionCost += cost
		sourceStats.SessionCostUsd = sourceStats.SessionCost
		sourceStats.SessionCurrency = usage.Currency
	}
	s.Sources[source] = sourceStats
}

func (s *UsageStats) cacheTokenDelta(source string, usage *eventwire.Usage) (hit, miss int) {
	hit, miss = usage.CacheHitTokens, usage.CacheMissTokens
	if source != event.UsageSourceExecutor && source != event.UsageSourcePlanner {
		return hit, miss
	}
	sessionHit, sessionMiss := usage.SessionCacheHitTokens, usage.SessionCacheMissTokens
	if sessionHit+sessionMiss <= 0 {
		return hit, miss
	}
	if s.SourceSessionCache == nil {
		s.SourceSessionCache = map[string]SourceSessionCacheCounters{}
	}
	previous, ok := s.SourceSessionCache[source]
	s.SourceSessionCache[source] = SourceSessionCacheCounters{Hit: sessionHit, Miss: sessionMiss}
	if !ok {
		return sessionHit, sessionMiss
	}
	if sessionHit < previous.Hit || sessionMiss < previous.Miss {
		if hit+miss > 0 {
			return hit, miss
		}
		return sessionHit, sessionMiss
	}
	return sessionHit - previous.Hit, sessionMiss - previous.Miss
}

// At returns the immutable view used for persistence and snapshots. Runtime
// bookkeeping is removed from the returned value.
func (s UsageStats) At(nowMs int64) UsageStats {
	out := s.Clone()
	if started := out.ActiveTurnStartedAt; started > 0 && nowMs >= started {
		out.ElapsedMs += nowMs - started
	}
	out.ActiveTurnStartedAt = 0
	out.SourceSessionCache = nil
	return out
}

// ReadFileFromEvent parses a successful read_file tool result. The bool is
// false for every other event. workspaceRoot is used only to normalize an
// absolute in-workspace path into the relative path required by Remote.
func ReadFileFromEvent(value eventwire.Event, turn int, nowMs int64, workspaceRoot string) (ReadFileRecord, bool) {
	if value.Kind != "tool_result" || value.Tool == nil || value.Tool.Name != "read_file" || value.Tool.Err != "" {
		return ReadFileRecord{}, false
	}
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	path, offset, limit := value.Tool.Args, 0, 0
	if json.Unmarshal([]byte(value.Tool.Args), &args) == nil && args.Path != "" {
		path, offset, limit = args.Path, args.Offset, args.Limit
	}
	path = normalizeReadPath(path, workspaceRoot)
	return ReadFileRecord{
		Path: path, Turn: turn, Time: nowMs, Offset: offset, Limit: limit,
		Truncated: value.Tool.Truncated || strings.Contains(value.Tool.Output, "truncated") || strings.Contains(value.Tool.Output, "File truncated"),
	}, true
}

func normalizeReadPath(path, workspaceRoot string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." {
		return ""
	}
	root := filepath.Clean(strings.TrimSpace(workspaceRoot))
	if !filepath.IsAbs(path) || root == "." || !filepath.IsAbs(root) {
		return filepath.ToSlash(path)
	}
	relative, err := filepath.Rel(root, path)
	if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

// View builds a stable snapshot from live state.
func View(readFiles []ReadFileRecord, usage UsageStats, nowMs int64) Snapshot {
	result := Snapshot{Version: Version, ReadFiles: append([]ReadFileRecord(nil), readFiles...), Usage: usage.At(nowMs)}
	if result.ReadFiles == nil {
		result.ReadFiles = []ReadFileRecord{}
	}
	return result
}

func Path(sessionPath string) string { return sessionPath + ".telemetry.json" }

// Save atomically replaces the telemetry sidecar and preserves an existing
// regular file's mode. The runtime-only fields are excluded by JSON tags.
func Save(path string, snapshot Snapshot) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("session telemetry path is empty")
	}
	snapshot = snapshot.Clone()
	if snapshot.Version == 0 {
		snapshot.Version = Version
	}
	if snapshot.ReadFiles == nil {
		snapshot.ReadFiles = []ReadFileRecord{}
	}
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	temporary, err := os.CreateTemp(dir, ".telemetry.*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(body); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	return fileutil.ReplaceFile(temporaryPath, path)
}

// Load accepts the current object format and the legacy array-only read-file
// sidecar. Missing or malformed files preserve the established empty behavior.
func Load(path string) Snapshot {
	body, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return Snapshot{Version: Version, ReadFiles: []ReadFileRecord{}}
	}
	var snapshot Snapshot
	if json.Unmarshal(body, &snapshot) == nil && (snapshot.Version > 0 || snapshot.ReadFiles != nil) {
		if snapshot.ReadFiles == nil {
			snapshot.ReadFiles = []ReadFileRecord{}
		}
		if snapshot.Usage.SessionCost == 0 && snapshot.Usage.SessionCostUsd > 0 {
			snapshot.Usage.SessionCost = snapshot.Usage.SessionCostUsd
		}
		return snapshot
	}
	var records []ReadFileRecord
	if json.Unmarshal(body, &records) != nil || records == nil {
		records = []ReadFileRecord{}
	}
	return Snapshot{Version: 1, ReadFiles: records}
}

func NowMillis() int64 { return time.Now().UnixMilli() }
