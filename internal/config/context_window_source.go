package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Context window source markers. Explicit windows are user-owned and never
// overridden by provider observations; default placeholders (wizard default or
// blank) may be replaced by a learned window from a trusted context-limit error.
const (
	ContextWindowSourceExplicit = "explicit"
	ContextWindowSourceDefault  = "default"
)

// EffectiveContextWindowSource returns the window source the harness should
// honour. A missing marker is treated as explicit when a positive window is
// configured (backward compatible), and as a default placeholder otherwise.
func EffectiveContextWindowSource(e *ProviderEntry) string {
	if e == nil {
		return ContextWindowSourceDefault
	}
	switch strings.TrimSpace(e.ContextWindowSource) {
	case ContextWindowSourceExplicit, ContextWindowSourceDefault:
		return strings.TrimSpace(e.ContextWindowSource)
	}
	if e.ContextWindow > 0 {
		return ContextWindowSourceExplicit
	}
	return ContextWindowSourceDefault
}

// LearnedWindow is a provider-observed context budget, keyed by base URL + model.
type LearnedWindow struct {
	WindowTokens     int       `json:"windowTokens"`
	CompletionBudget int       `json:"completionBudget,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// LearnedWindowStore persists provider-observed context budgets across
// restarts. Only downward window updates are accepted; keys never contain
// credentials.
type LearnedWindowStore struct {
	mu   sync.Mutex
	path string
	data map[string]LearnedWindow
}

// LearnedWindowKey normalises (baseURL, model) into a stable store key.
func LearnedWindowKey(baseURL, model string) string {
	baseURL = strings.ToLower(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	return baseURL + "|" + strings.TrimSpace(model)
}

// LoadLearnedWindowStore loads (or creates) the store under ReasonixHomeDir.
func LoadLearnedWindowStore() (*LearnedWindowStore, error) {
	dir := ReasonixHomeDir()
	if dir == "" {
		return &LearnedWindowStore{data: map[string]LearnedWindow{}}, nil
	}
	return LoadLearnedWindowStorePath(filepath.Join(dir, "context-window-learned.json"))
}

// LoadLearnedWindowStorePath loads a store from an explicit path (tests).
func LoadLearnedWindowStorePath(path string) (*LearnedWindowStore, error) {
	s := &LearnedWindowStore{path: path, data: map[string]LearnedWindow{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, err
	}
	if s.data == nil {
		s.data = map[string]LearnedWindow{}
	}
	return s, nil
}

// Get returns the learned budget for a key, or zero values when absent.
func (s *LearnedWindowStore) Get(baseURL, model string) LearnedWindow {
	if s == nil {
		return LearnedWindow{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[LearnedWindowKey(baseURL, model)]
}

// Update records a downward window observation and persists atomically.
// Completion budgets only ever grow upward from provider evidence.
func (s *LearnedWindowStore) Update(baseURL, model string, w LearnedWindow) (LearnedWindow, bool) {
	if s == nil || w.WindowTokens <= 0 {
		return LearnedWindow{}, false
	}
	key := LearnedWindowKey(baseURL, model)
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.data[key]
	if prev.WindowTokens > 0 && w.WindowTokens >= prev.WindowTokens {
		return prev, false
	}
	next := LearnedWindow{
		WindowTokens:     w.WindowTokens,
		CompletionBudget: w.CompletionBudget,
		UpdatedAt:        time.Now(),
	}
	if prev.CompletionBudget > next.CompletionBudget {
		next.CompletionBudget = prev.CompletionBudget
	}
	s.data[key] = next
	if s.path != "" {
		if err := s.saveLocked(); err != nil {
			return prev, false
		}
	}
	return next, true
}

func (s *LearnedWindowStore) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
