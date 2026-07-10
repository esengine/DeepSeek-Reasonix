package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// titleCache persists generated session titles to <dir>/.session-titles.json,
// keyed by file name and invalidated by mtime, so the flash model is queried
// once per session rather than on every sidebar refresh. Persistence is
// best-effort: a missing or unreadable cache just regenerates.
type titleCache struct {
	mu      sync.Mutex
	dir     string
	loaded  bool
	entries map[string]titleEntry
}

type titleEntry struct {
	Title string `json:"title"`
	Mod   int64  `json:"mod"`
}

func newTitleCache(dir string) *titleCache {
	return &titleCache{dir: dir, entries: map[string]titleEntry{}}
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

func (c *titleCache) get(name string, mod int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	if e, ok := c.entries[name]; ok && e.Mod == mod {
		return e.Title, true
	}
	return "", false
}

func (c *titleCache) put(name, title string, mod int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	c.entries[name] = titleEntry{Title: title, Mod: mod}
	if data, err := json.Marshal(c.entries); err == nil {
		_ = os.WriteFile(filepath.Join(c.dir, ".session-titles.json"), data, 0o644)
	}
}
