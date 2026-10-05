package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// legacyCache remembers which conversations were kept by Reasonix 1.x. Deciding
// that means reading the session's event log head, and the sidebar asks for
// every row on every render, so the answer is cached beside the project's own
// session files under .session-legacy.json.
type legacyCache struct {
	mu      sync.Mutex
	dir     string
	loaded  bool
	entries map[string]legacyEntry
}

type legacyEntry struct {
	Legacy bool  `json:"legacy"`
	Size   int64 `json:"size"`
	Mod    int64 `json:"mod"`
}

func newLegacyCache(dir string) *legacyCache {
	return &legacyCache{dir: dir, entries: map[string]legacyEntry{}}
}

// legacyOf reports whether the session at sessionPath is a 1.x conversation,
// reusing the cached answer while the event log it was decided from is
// unchanged.
func (c *legacyCache) legacyOf(sessionPath string) bool {
	size, mod := sessionEventLogStamp(sessionPath)
	name := filepath.Base(sessionPath)
	if legacy, ok := c.cached(name, size, mod); ok {
		return legacy
	}
	legacy := sessionstore.IsForeignSessionLog(sessionPath)
	c.put(name, legacy, size, mod)
	return legacy
}

// sessionEventLogStamp is the cache key: the event log's own size and mtime, or
// a sentinel while there is no log, so the log's first appearance is a miss.
func sessionEventLogStamp(sessionPath string) (int64, int64) {
	info, err := os.Stat(store.SessionEventLog(sessionPath))
	if err != nil {
		return 0, -1
	}
	return info.Size(), info.ModTime().UnixNano()
}

func (c *legacyCache) cached(name string, size, mod int64) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	e, ok := c.entries[name]
	if !ok || e.Size != size || e.Mod != mod {
		return false, false
	}
	return e.Legacy, true
}

func (c *legacyCache) put(name string, legacy bool, size, mod int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.entries[name] = legacyEntry{Legacy: legacy, Size: size, Mod: mod}
	if data, err := json.Marshal(c.entries); err == nil {
		_ = os.WriteFile(filepath.Join(c.dir, ".session-legacy.json"), data, 0o644)
	}
}

func (c *legacyCache) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	if data, err := fileencoding.ReadFileUTF8(filepath.Join(c.dir, ".session-legacy.json")); err == nil {
		_ = json.Unmarshal(data, &c.entries)
	}
}
