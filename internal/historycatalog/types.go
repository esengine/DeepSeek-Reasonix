// Package historycatalog maintains a disposable FTS projection of saved
// session history. Session JSONL/event/meta files remain authoritative.
package historycatalog

import (
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/projectiondb"
)

const (
	SchemaVersion    = 1
	TokenizerVersion = 1
	DefaultLimit     = 50
	MaxLimit         = 200
)

type Status struct {
	State           string            `json:"state"`
	Mode            projectiondb.Mode `json:"mode"`
	Path            string            `json:"path,omitempty"`
	Revision        uint64            `json:"revision"`
	Indexed         int64             `json:"indexed"`
	Total           int64             `json:"total"`
	Pending         int64             `json:"pending"`
	Failed          int64             `json:"failed"`
	LastError       string            `json:"lastError,omitempty"`
	QuarantinedPath string            `json:"quarantinedPath,omitempty"`
}

type Options struct {
	Path              string
	InMemory          bool
	QueueCapacity     int
	MissingGrace      time.Duration
	ReconcileInterval time.Duration
	Now               func() time.Time
	OnRevision        func(Status, []string, string)
}

type Root struct {
	Path          string
	Source        string
	Scope         string
	WorkspaceRoot string
	Subagents     bool
	Archive       bool
}

type Candidate struct {
	RowID          int64
	SessionPath    string
	Root           string
	Source         string
	Scope          string
	WorkspaceRoot  string
	ContentDigest  string
	MessageIndex   int
	PartIndex      int
	Role           string
	Kind           string
	ToolName       string
	Rank           float64
	Score          float64
	SessionTitle   string
	TopicTitle     string
	LastActivityAt int64
}

type SearchRequest struct {
	Query         string
	Scope         string
	WorkspaceRoot string
	Kinds         []string
	ToolName      string
	Limit         int
	Roots         []string
	After         *SearchCursor
}

// SearchCursor is a stable keyset over SQLite's BM25 rank and deterministic
// tie-break keys. Lower BM25 ranks sort first.
type SearchCursor struct {
	Rank         float64
	SessionPath  string
	MessageIndex int
	PartIndex    int
	RowID        int64
}

type SearchResult struct {
	Items    []Candidate
	Revision uint64
	Partial  bool
}

// DefaultPath returns the disposable history FTS path under CacheDir.
// Empty when the OS cache directory is unavailable so Open falls back to memory.
func DefaultPath() string {
	cache := strings.TrimSpace(config.CacheDir())
	if cache == "" {
		return ""
	}
	return filepath.Join(cache, "history-search", "v1.sqlite")
}
