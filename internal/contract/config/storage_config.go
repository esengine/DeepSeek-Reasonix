// storage_config.go — the configured layer of the root precedence chain.
package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

// storageSection is the [storage] table: root id to directory. A map rather
// than a field per root, so declaring a new root in storageRoots is the only
// edit a new relocatable root needs.
type storageSection struct {
	Storage map[string]string `toml:"storage"`
}

// storageDirCache holds the [storage] table per home root. Roots resolve on
// nearly every path call, so each file is read once — but keyed by the home
// that produced it, because an isolated instance (and every test) moves home
// out from under a process that has already resolved paths, and one process
// may resolve against several stated homes at once.
var storageDirCache struct {
	mu    sync.Mutex
	byOwn map[string]map[RootID]string
}

// configuredRootDir answers what the configuration chose for a root, "" when it
// chose nothing. An immovable root always answers "": home cannot be named by
// the file it holds, and the locks root must not follow a profile at all.
func (r Roots) configuredRootDir(id RootID) string {
	if !RootRelocatable(id) {
		return ""
	}
	home := r.Dir(RootHome)
	if home == "" {
		return ""
	}
	storageDirCache.mu.Lock()
	defer storageDirCache.mu.Unlock()
	dirs, ok := storageDirCache.byOwn[home]
	if !ok {
		dirs = readStorageDirs(filepath.Join(home, "config.toml"))
		if storageDirCache.byOwn == nil {
			storageDirCache.byOwn = map[string]map[RootID]string{}
		}
		storageDirCache.byOwn[home] = dirs
	}
	return dirs[id]
}

// InvalidateStorageDirs drops the cached [storage] tables. A process that just
// wrote one calls this so a later read sees it; the resolved roots a running
// runtime already handed out do not change, which is why relocation takes
// effect on the next launch rather than mid-session.
func InvalidateStorageDirs() {
	storageDirCache.mu.Lock()
	defer storageDirCache.mu.Unlock()
	storageDirCache.byOwn = nil
}

// readStorageDirs decodes only the [storage] table. A malformed or unreadable
// config answers an empty set rather than an error: paths must still resolve
// for a user whose config file is broken, or they could not reach the tooling
// that repairs it.
func readStorageDirs(path string) map[RootID]string {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return nil
	}
	var section storageSection
	if _, err := toml.Decode(string(b), &section); err != nil {
		return nil
	}
	if len(section.Storage) == 0 {
		return nil
	}
	out := make(map[RootID]string, len(section.Storage))
	for key, dir := range section.Storage {
		id := RootID(strings.TrimSpace(key))
		if !RootRelocatable(id) {
			// A configuration naming an immovable root is not an error worth
			// failing a launch over; it simply does not get to move it.
			continue
		}
		if dir := cleanConfiguredDir(dir); dir != "" {
			out[id] = dir
		}
	}
	return out
}

// cleanConfiguredDir expands and absolutises a configured directory the same
// way an environment override is treated, so the two layers of the chain accept
// the same spellings — "~/x", "%LOCALAPPDATA%\x", a relative path.
func cleanConfiguredDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return expandDirValue(dir)
}

// SetStorageDir relocates one root, or with "" returns it to its default. It
// rules on what the configuration can rule on — that the root exists, may be
// moved, and is not held by the environment — and leaves whether the target
// fits, is reachable, and is not inside the source to the move that acts on it.
func (c *Config) SetStorageDir(id RootID, dir string) error {
	root, ok := lookupRoot(id)
	if !ok {
		return fmt.Errorf("set storage: no root named %q", id)
	}
	if !root.relocatable {
		return fmt.Errorf("set storage: %s cannot be moved", id)
	}
	if pin := RootPinnedBy(id); pin != "" {
		return fmt.Errorf("set storage: %s is held by %s; unset it to choose a location here", id, pin)
	}
	dir = expandDirValue(dir)
	if dir == "" {
		delete(c.Storage, string(id))
		if len(c.Storage) == 0 {
			c.Storage = nil
		}
		return nil
	}
	if c.Storage == nil {
		c.Storage = map[string]string{}
	}
	c.Storage[string(id)] = dir
	return nil
}

// StorageDir reports the configured location for a root, "" when the
// configuration names none and the root falls back to its default.
func (c *Config) StorageDir(id RootID) string {
	return strings.TrimSpace(c.Storage[string(id)])
}
