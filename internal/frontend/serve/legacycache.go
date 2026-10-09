package serve

import (
	"os"
	"path/filepath"

	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

// legacyCache remembers which conversations were kept by Reasonix 1.x: the
// answer needs the session's event log head, and the sidebar asks for every
// row on every render. Persistence lives in stampFile under
// .session-legacy.json, written once per listing.
type legacyCache struct {
	file *stampFile[legacyEntry]
}

type legacyEntry struct {
	Legacy bool  `json:"legacy"`
	Size   int64 `json:"size"`
	Mod    int64 `json:"mod"`
}

func newLegacyCache(dir string) *legacyCache {
	return &legacyCache{file: newStampFile[legacyEntry](filepath.Join(dir, ".session-legacy.json"))}
}

// legacyOf reports whether the session at sessionPath is a 1.x conversation,
// reusing the cached answer while the event log it was decided from is
// unchanged.
func (c *legacyCache) legacyOf(sessionPath string) bool {
	size, mod := sessionEventLogStamp(sessionPath)
	name := filepath.Base(sessionPath)
	if e, ok := c.file.entry(name); ok && e.Size == size && e.Mod == mod {
		return e.Legacy
	}
	legacy := sessionstore.IsForeignSessionLog(sessionPath)
	c.file.record(name, legacyEntry{Legacy: legacy, Size: size, Mod: mod})
	return legacy
}

// flush writes the file at most once per sidebar listing.
func (c *legacyCache) flush() {
	c.file.flush()
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
