package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/session"
)

// titleCache is a disposable edit-avoidance cache for generated session titles,
// persisted to <dir>/.session-titles.json. Entries are keyed by the session's
// legacy transcript file name or, for a v4 session with no transcript file, its
// store id. Validity comes from the first user message: appending turns changes
// mtime without invalidating the title, while replacing the first turn (for
// example by rewinding turn zero) produces a cache miss.
//
// This file is NOT the canonical home of a session title. The store owns that:
// a "session/title" event in the v4 event log, written by the agent's title
// tool or by Service.SetTitle, which is what a listing must prefer. This cache
// exists only so Serve does not re-spend a generation request whenever the
// first message is unchanged; losing it costs requests, never information.
type titleCache struct {
	mu      sync.Mutex
	dir     string
	loaded  bool
	entries map[string]titleEntry
}

type titleEntry struct {
	Title      string `json:"title"`
	Mod        int64  `json:"mod"`
	SourceHash string `json:"source_hash,omitempty"`
}

func newTitleCache(dir string) *titleCache {
	return &titleCache{dir: dir, entries: map[string]titleEntry{}}
}

// titleCacheDir maps the controller's legacy transcript catalog to the store
// root that holds both v4 sessions and this cache. SessionDir is always that
// catalog (a sibling "sessions-v4" store is derived from it everywhere else
// too), and the cache must sit in the store: a Serve process that wrote titles
// beside the legacy catalog could not read them back for the v4 sessions they
// describe.
func titleCacheDir(sessionDir string) string {
	return session.RootForLegacyDir(sessionDir)
}

func (c *titleCache) load() {
	if c.loaded {
		return
	}
	c.loaded = true
	if data, err := fileencoding.ReadFileUTF8(filepath.Join(c.dir, ".session-titles.json")); err == nil {
		_ = json.Unmarshal(data, &c.entries)
	}
}

func titleSourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func (c *titleCache) get(name, source string, mod int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	e, ok := c.entries[name]
	if !ok {
		return "", false
	}
	if e.SourceHash == "" {
		// Legacy entries used only mtime. Accept a still-current entry without
		// rewriting the cache; the next transcript append regenerates once and
		// upgrades it to source_hash automatically.
		if e.Mod == mod {
			return e.Title, true
		}
		return "", false
	}
	if e.SourceHash == titleSourceHash(source) {
		return e.Title, true
	}
	return "", false
}

func (c *titleCache) put(name, title, source string, mod int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.entries[name] = titleEntry{Title: title, Mod: mod, SourceHash: titleSourceHash(source)}
	if data, err := json.Marshal(c.entries); err == nil {
		_ = os.WriteFile(filepath.Join(c.dir, ".session-titles.json"), data, 0o644)
	}
}
