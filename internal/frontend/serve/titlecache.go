package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/base/fileutil"
	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/state/sessionstore"
)

// Every reader reloads under the same lock: panes and the tree have separate
// cache handles, and a stale writer must not undo an explicit rename.
var titleCacheMu sync.Mutex

type titleCache struct{ dir string }

type titleEntry struct {
	Title      string `json:"title"`
	Mod        int64  `json:"mod"`
	SourceHash string `json:"source_hash,omitempty"`
	Explicit   bool   `json:"explicit,omitempty"`
}

func newTitleCache(dir string) *titleCache { return &titleCache{dir: dir} }

func (c *titleCache) setDir(dir string) {
	titleCacheMu.Lock()
	defer titleCacheMu.Unlock()
	c.dir = dir
}

func (c *titleCache) load() (map[string]titleEntry, error) {
	entries := map[string]titleEntry{}
	data, err := fileencoding.ReadFileUTF8(filepath.Join(c.dir, ".session-titles.json"))
	if errors.Is(err, os.ErrNotExist) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return map[string]titleEntry{}, nil
	}
	if entries == nil {
		entries = map[string]titleEntry{}
	}
	return entries, nil
}

func titleSourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func (c *titleCache) get(name, source string, mod int64) (string, bool) {
	title := c.display(name, source, mod, "")
	return title, title != ""
}

type titleSnapshot map[string]titleEntry

func (c *titleCache) snapshot() titleSnapshot {
	titleCacheMu.Lock()
	defer titleCacheMu.Unlock()
	entries, _ := c.load()
	return titleSnapshot(entries)
}

func (c *titleCache) display(name, source string, mod int64, custom string) string {
	return c.snapshot().display(name, source, mod, custom)
}

func (snapshot titleSnapshot) display(name, source string, mod int64, custom string) string {
	if custom = strings.TrimSpace(custom); custom != "" {
		return custom
	}
	entry := snapshot[name]
	if entry.Explicit {
		return entry.Title
	}
	if entry.SourceHash != "" {
		if entry.SourceHash == titleSourceHash(source) {
			return entry.Title
		}
	} else if entry.Mod == mod {
		return entry.Title
	}
	return ""
}

func (c *titleCache) put(name, title, source string, mod int64) {
	_ = c.store(name, titleEntry{Title: title, Mod: mod, SourceHash: titleSourceHash(source)})
}

func (c *titleCache) putExplicit(name, title string) error {
	return c.store(name, titleEntry{Title: title, Explicit: true})
}

func (c *titleCache) clearExplicit(name string) error {
	titleCacheMu.Lock()
	defer titleCacheMu.Unlock()
	entries, err := c.load()
	if err != nil {
		return err
	}
	if entries[name].Explicit {
		delete(entries, name)
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return writeSessionTitle(filepath.Join(c.dir, name), "", func() error {
		return fileutil.AtomicWriteFile(filepath.Join(c.dir, ".session-titles.json"), data, 0o644)
	})
}

func (c *titleCache) store(name string, entry titleEntry) error {
	titleCacheMu.Lock()
	defer titleCacheMu.Unlock()
	entries, err := c.load()
	if err != nil {
		return err
	}
	if entries[name].Explicit && !entry.Explicit {
		return nil
	}
	entries[name] = entry
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	writeCache := func() error {
		return fileutil.AtomicWriteFile(filepath.Join(c.dir, ".session-titles.json"), data, 0o644)
	}
	if entry.Explicit {
		return writeSessionTitle(filepath.Join(c.dir, name), entry.Title, writeCache)
	}
	return writeCache()
}

// Hold the metadata lock through both writes and rollback so a failed cache
// write cannot undo metadata saved by another runtime.
func writeSessionTitle(path, title string, writeCache func() error) error {
	unlock, err := sessionstore.LockSessionMetaPath(path)
	if err != nil {
		return err
	}
	defer unlock()

	metaPath := sessionstore.BranchMetaPath(path)
	original, readErr := os.ReadFile(metaPath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	originalMode := os.FileMode(0o600)
	if readErr == nil {
		info, err := os.Stat(metaPath)
		if err != nil {
			return err
		}
		originalMode = info.Mode().Perm()
	}
	meta, _, err := sessionstore.LoadBranchMeta(path)
	if err != nil {
		return err
	}
	meta.CustomTitle = title
	if err := sessionstore.SaveBranchMetaPreserveUpdatedLocked(path, meta); err != nil {
		return err
	}
	if err = writeCache(); err == nil {
		return nil
	}

	var rollbackErr error
	if errors.Is(readErr, os.ErrNotExist) {
		rollbackErr = os.Remove(metaPath)
	} else {
		rollbackErr = fileutil.AtomicWriteFile(metaPath, original, originalMode)
	}
	return errors.Join(err, rollbackErr)
}
