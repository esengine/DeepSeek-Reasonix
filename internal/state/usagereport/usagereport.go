// Package usagereport is the per-session token record the host writes beside a
// session and `reasonix doctor` reads back. It is one declaration because the
// writer and the reader are different layers that meet only over this JSON: a
// field renamed on one side and not the other reads as zero forever.
package usagereport

import (
	"encoding/json"
	"os"

	"reasonix/internal/base/fileutil"
)

// Version is the schema this package writes. Readers accept older ones.
const Version = 1

// Report is one session's accounting.
type Report struct {
	Version int   `json:"version"`
	Usage   Usage `json:"usage"`
}

// Usage totals every model request the session billed, and splits the count by
// where the request came from so a report says which component spent it.
type Usage struct {
	PromptTokens     int               `json:"promptTokens"`
	CompletionTokens int               `json:"completionTokens"`
	ReasoningTokens  int               `json:"reasoningTokens"`
	CacheHitTokens   int               `json:"cacheHitTokens"`
	CacheMissTokens  int               `json:"cacheMissTokens"`
	Estimated        bool              `json:"estimated,omitempty"`
	RequestCount     int               `json:"requestCount"`
	Sources          map[string]Source `json:"sources,omitempty"`
	ColdStart        *ColdStart        `json:"coldStart,omitempty"`
}

// Source is one origin's share of the request count.
type Source struct {
	RequestCount int `json:"requestCount"`
}

// ColdStart is the session's first executor request, apart from the totals it
// is also in. Later requests reuse the prefix it paid for, so the session rate
// says whether the cache worked once warm, never how much a cold start reused.
// Planner, subagent, compaction, classifier and title requests are excluded:
// they carry their own prefixes.
type ColdStart struct {
	PromptTokens    int `json:"promptTokens"`
	CacheHitTokens  int `json:"cacheHitTokens"`
	CacheMissTokens int `json:"cacheMissTokens"`
}

// Path is where a session's report lives, given the session's own path.
func Path(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return sessionPath + ".telemetry.json"
}

// Save writes the report for sessionPath, atomically so a reader never sees a
// half-written record. A session with no path (a headless run) writes nothing.
func Save(sessionPath string, r Report) error {
	path := Path(sessionPath)
	if path == "" {
		return nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, append(data, '\n'), 0o600)
}

// Load reads the report for sessionPath. A missing or unreadable file is not
// an error to the caller: it reports that there is nothing to say.
func Load(sessionPath string) (Report, bool) {
	path := Path(sessionPath)
	if path == "" {
		return Report{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, false
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, false
	}
	return r, true
}
