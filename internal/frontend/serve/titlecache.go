package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// titleCache persists generated session titles to <dir>/.session-titles.json.
// Entries are keyed by file name and the first user message: appending turns
// changes the transcript mtime without invalidating the title, while replacing
// the first turn (for example by rewinding turn zero) produces a cache miss.
// Persistence itself lives in stampFile: load once, write once per batch.
type titleCache struct {
	file *stampFile[titleEntry]
}

type titleEntry struct {
	Title      string `json:"title"`
	Mod        int64  `json:"mod"`
	SourceHash string `json:"source_hash,omitempty"`
}

func newTitleCache(dir string) *titleCache {
	return &titleCache{file: newStampFile[titleEntry](filepath.Join(dir, ".session-titles.json"))}
}

// setDir repoints the cache at another session directory and drops what was
// loaded from the previous one: entries are keyed by file name, which is
// unique within a project and not across them.
func (c *titleCache) setDir(dir string) {
	c.file.setPath(filepath.Join(dir, ".session-titles.json"))
}

func titleSourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func (c *titleCache) get(name, source string, mod int64) (string, bool) {
	e, ok := c.file.entry(name)
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
	c.file.record(name, titleEntry{Title: title, Mod: mod, SourceHash: titleSourceHash(source)})
}

// flush writes the file at most once per batch of generated titles.
func (c *titleCache) flush() {
	c.file.flush()
}
