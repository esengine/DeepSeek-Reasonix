package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

// SourceBreakdown tracks token usage and cost for a specific source within a session.
type SourceBreakdown struct {
	Source           string  `json:"source"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	Calls            int     `json:"calls"`
}

// SessionUsage holds cumulative token usage, cost, and activity data for a single session.
type SessionUsage struct {
	SessionID        string            `json:"session_id"`
	PromptTokens     int               `json:"prompt_tokens"`
	CompletionTokens int               `json:"completion_tokens"`
	TotalTokens      int               `json:"total_tokens"`
	CacheHitTokens   int               `json:"cache_hit_tokens"`
	CacheMissTokens  int               `json:"cache_miss_tokens"`
	ReasoningTokens  int               `json:"reasoning_tokens"`
	Cost             float64           `json:"cost"`
	Currency         string            `json:"currency"`
	Turns            int               `json:"turns"`
	StartedAt        time.Time         `json:"started_at"`
	LastActivity     time.Time         `json:"last_activity"`
	Model            string            `json:"model"`
	Breakdown        []SourceBreakdown `json:"breakdown,omitempty"`
	CodeAdded        int               `json:"code_added"`
	CodeRemoved      int               `json:"code_removed"`
}

// Tracker manages per-session usage tracking with thread-safe access and persistent storage.
type Tracker struct {
	baseDir  string
	mu       sync.RWMutex
	sessions map[string]*SessionUsage
	currency string
}

// NewTracker creates a new usage tracker that persists session data under baseDir.
func NewTracker(baseDir string) *Tracker {
	return &Tracker{
		baseDir:  baseDir,
		sessions: map[string]*SessionUsage{},
		currency: "¥",
	}
}

func (t *Tracker) sessionPath(id string) string {
	return filepath.Join(t.baseDir, "usage", id+".json")
}

// LoadSession reads a session's usage data from disk. Returns nil, nil if the file does not exist.
func (t *Tracker) LoadSession(id string) (*SessionUsage, error) {
	path := t.sessionPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var su SessionUsage
	if err := json.Unmarshal(data, &su); err != nil {
		return nil, err
	}
	return &su, nil
}

func (t *Tracker) saveSession(su *SessionUsage) error {
	dir := filepath.Join(t.baseDir, "usage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(su, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := t.sessionPath(su.SessionID)
	tmp, err := os.CreateTemp(dir, ".usage-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return fileutil.ReplaceFile(tmpPath, path)
}

// Record adds a usage entry to the specified session, updating token counts, cost, and source breakdown.
func (t *Tracker) Record(sessionID string, usage *provider.Usage, pricing *provider.Pricing, source string, model string) {
	if usage == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	su, ok := t.sessions[sessionID]
	if !ok {
		su = &SessionUsage{
			SessionID: sessionID,
			StartedAt: time.Now().UTC(),
			Model:     model,
			Currency:  t.currency,
		}
		t.sessions[sessionID] = su
	}

	su.PromptTokens += usage.PromptTokens
	su.CompletionTokens += usage.CompletionTokens
	su.TotalTokens += usage.TotalTokens
	su.CacheHitTokens += usage.CacheHitTokens
	su.CacheMissTokens += usage.CacheMissTokens
	su.ReasoningTokens += usage.ReasoningTokens
	su.Turns++
	su.LastActivity = time.Now().UTC()
	if model != "" {
		su.Model = model
	}
	if pricing != nil {
		su.Cost += pricing.Cost(usage)
		su.Currency = pricing.Symbol()
	}

	if source != "" {
		found := false
		for i := range su.Breakdown {
			if su.Breakdown[i].Source == source {
				su.Breakdown[i].PromptTokens += usage.PromptTokens
				su.Breakdown[i].CompletionTokens += usage.CompletionTokens
				su.Breakdown[i].TotalTokens += usage.TotalTokens
				if pricing != nil {
					su.Breakdown[i].Cost += pricing.Cost(usage)
				}
				su.Breakdown[i].Calls++
				found = true
				break
			}
		}
		if !found {
			cost := 0.0
			if pricing != nil {
				cost = pricing.Cost(usage)
			}
			su.Breakdown = append(su.Breakdown, SourceBreakdown{
				Source:           source,
				PromptTokens:     usage.PromptTokens,
				CompletionTokens: usage.CompletionTokens,
				TotalTokens:      usage.TotalTokens,
				Cost:             cost,
				Calls:            1,
			})
		}
	}

	_ = t.saveSession(su)
}

// RecordCodeChanges tracks the number of lines added and removed during a session.
func (t *Tracker) RecordCodeChanges(sessionID string, added, removed int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	su, ok := t.sessions[sessionID]
	if !ok {
		su = &SessionUsage{
			SessionID: sessionID,
			StartedAt: time.Now().UTC(),
			Currency:  t.currency,
		}
		t.sessions[sessionID] = su
	}
	su.CodeAdded += added
	su.CodeRemoved += removed
	su.LastActivity = time.Now().UTC()
	_ = t.saveSession(su)
}

// Get returns a copy of the session usage data for the given session ID, and whether it exists.
func (t *Tracker) Get(sessionID string) (*SessionUsage, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	su, ok := t.sessions[sessionID]
	if !ok {
		return nil, false
	}
	copy := *su
	return &copy, true
}

// ListRecent returns sessions sorted by last activity, most recent first, up to the specified limit.
func (t *Tracker) ListRecent(limit int) []SessionUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	sessions := make([]SessionUsage, 0, len(t.sessions))
	for _, su := range t.sessions {
		sessions = append(sessions, *su)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity.After(sessions[j].LastActivity)
	})
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions
}

// TotalCost returns the sum of costs across all tracked sessions.
func (t *Tracker) TotalCost() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := 0.0
	for _, su := range t.sessions {
		total += su.Cost
	}
	return total
}

// TotalTokens returns the sum of total tokens across all tracked sessions.
func (t *Tracker) TotalTokens() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := 0
	for _, su := range t.sessions {
		total += su.TotalTokens
	}
	return total
}

// Summary returns a human-readable summary of aggregate usage across all sessions.
func (t *Tracker) Summary() string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var totalPrompt, totalCompletion, totalCacheHit, totalCacheMiss int
	var totalCost float64
	var sessions int

	for _, su := range t.sessions {
		totalPrompt += su.PromptTokens
		totalCompletion += su.CompletionTokens
		totalCacheHit += su.CacheHitTokens
		totalCacheMiss += su.CacheMissTokens
		totalCost += su.Cost
		sessions++
	}

	currency := t.currency
	if len(t.sessions) > 0 {
		for _, su := range t.sessions {
			if su.Currency != "" {
				currency = su.Currency
				break
			}
		}
	}

	return fmt.Sprintf(
		"Total cost: %s%.2f  Sessions: %d  Prompt: %d  Completion: %d  Cache hit: %d  Cache miss: %d",
		currency, totalCost, sessions,
		totalPrompt, totalCompletion,
		totalCacheHit, totalCacheMiss,
	)
}

// FormatSessionUsage returns a formatted string with detailed usage information for a single session.
func (t *Tracker) FormatSessionUsage(id string) string {
	su, ok := t.Get(id)
	if !ok {
		return "No usage data for this session"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Session: %s\n", su.SessionID)
	fmt.Fprintf(&sb, "Total cost: %s%.4f\n", su.Currency, su.Cost)
	fmt.Fprintf(&sb, "Prompt tokens: %d\n", su.PromptTokens)
	fmt.Fprintf(&sb, "Completion tokens: %d\n", su.CompletionTokens)
	fmt.Fprintf(&sb, "Total tokens: %d\n", su.TotalTokens)
	if su.CacheHitTokens > 0 || su.CacheMissTokens > 0 {
		fmt.Fprintf(&sb, "Cache hit: %d  Cache miss: %d\n", su.CacheHitTokens, su.CacheMissTokens)
	}
	if su.ReasoningTokens > 0 {
		fmt.Fprintf(&sb, "Reasoning tokens: %d\n", su.ReasoningTokens)
	}
	fmt.Fprintf(&sb, "Turns: %d\n", su.Turns)
	fmt.Fprintf(&sb, "Code changes: +%d / -%d\n", su.CodeAdded, su.CodeRemoved)
	fmt.Fprintf(&sb, "Model: %s\n", su.Model)

	if len(su.Breakdown) > 0 {
		fmt.Fprintf(&sb, "\nBreakdown by source:\n")
		for _, b := range su.Breakdown {
			fmt.Fprintf(&sb, "  %s: %d tokens, %s%.4f (%d calls)\n",
				b.Source, b.TotalTokens, su.Currency, b.Cost, b.Calls)
		}
	}

	return sb.String()
}
