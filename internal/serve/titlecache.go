package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// titleCache persists generated session titles to <dir>/.session-titles.json.
// Entries are keyed by file name and the first user message: appending turns
// changes the transcript mtime without invalidating the title, while replacing
// the first turn (for example by rewinding turn zero) produces a cache miss.
// Persistence is best-effort: a missing or unreadable cache just regenerates.
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
