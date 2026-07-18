// Package sessiondisplay owns the durable user-message display sidecar shared
// by local Desktop sessions and Remote history capture. It deliberately knows
// nothing about topics, trash layout, or runtime lifecycle.
package sessiondisplay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"reasonix/internal/control"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// FileName is the sidecar stored beside Session transcript files.
const FileName = ".display.json"

// Map indexes Session transcript basenames, then SHA-256 hashes of canonical
// user-message content, to the shorter text shown to the user.
type Map map[string]map[string]string

// KeepFunc reports whether a sidecar Session key is still owned by the caller.
// It lets storage owners compose their own live, trash, and in-flight rules
// without teaching this neutral package those layouts. KeepFunc runs while the
// directory mutation lock is held and must not call back into this package for
// the same directory.
type KeepFunc func(sessionKey string) bool

// Path returns the display sidecar path for dir.
func Path(dir string) string {
	return filepath.Join(dir, FileName)
}

// MessageKey returns the stable content identity used in the sidecar format.
func MessageKey(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Load reads a complete sidecar snapshot. Missing, unreadable, or malformed
// files preserve the historical behavior of appearing empty.
func Load(dir string) Map {
	release := acquireDirectory(dir)
	defer release()
	return loadLocked(dir)
}

// Save atomically replaces the complete sidecar. A new sidecar is private to
// the current user; replacement preserves an existing regular file's mode.
func Save(dir string, displays Map) error {
	release := acquireDirectory(dir)
	defer release()
	return saveLocked(dir, displays)
}

// RemoveKey removes all display records for one Session transcript basename.
// Removing the final entry removes the empty sidecar itself.
func RemoveKey(dir, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	release := acquireDirectory(dir)
	defer release()

	displays := loadLocked(dir)
	if displays[key] == nil {
		return nil
	}
	delete(displays, key)
	return saveOrRemoveLocked(dir, displays)
}

// Remove removes all display records for sessionPath.
func Remove(dir, sessionPath string) error {
	if strings.TrimSpace(sessionPath) == "" {
		return nil
	}
	return RemoveKey(dir, filepath.Base(sessionPath))
}

// Record atomically merges one user-message display into the sidecar. The
// directory-scoped process lock covers the entire read-modify-write cycle, so
// concurrent Session controllers cannot overwrite each other's records.
func Record(dir, sessionPath, content, display string) error {
	if strings.TrimSpace(sessionPath) == "" || content == display || strings.TrimSpace(display) == "" {
		return nil
	}
	release := acquireDirectory(dir)
	defer release()

	displays := loadLocked(dir)
	key := filepath.Base(sessionPath)
	if displays[key] == nil {
		displays[key] = map[string]string{}
	}
	displays[key][MessageKey(content)] = display
	return saveLocked(dir, displays)
}

// Prune removes keys for which keep returns false. A nil keep function removes
// every key. The final removal deletes the empty sidecar.
func Prune(dir string, keep KeepFunc) error {
	release := acquireDirectory(dir)
	defer release()

	displays := loadLocked(dir)
	if len(displays) == 0 {
		return nil
	}
	changed := false
	for key := range displays {
		if keep != nil && keep(key) {
			continue
		}
		delete(displays, key)
		changed = true
	}
	if !changed {
		return nil
	}
	return saveOrRemoveLocked(dir, displays)
}

// Resolver loads the sidecar once and returns a per-message resolver.
func Resolver(dir, sessionPath string) func(content string) string {
	return ResolverFromMap(Load(dir), sessionPath)
}

// ResolverFromMap creates a resolver from an already-loaded sidecar snapshot.
func ResolverFromMap(displays Map, sessionPath string) func(content string) string {
	stored := displays[filepath.Base(sessionPath)]
	byHash := make(map[string]string, len(stored))
	for hash, display := range stored {
		byHash[hash] = display
	}
	return func(content string) string {
		if display := byHash[MessageKey(content)]; strings.TrimSpace(display) != "" {
			return display
		}
		return control.StripComposePrefixes(content)
	}
}

// Resolve loads and resolves one message display.
func Resolve(dir, sessionPath, content string) string {
	return Resolver(dir, sessionPath)(content)
}

func loadLocked(dir string) Map {
	displays := Map{}
	body, err := fileencoding.ReadFileUTF8(Path(dir))
	if err != nil {
		return displays
	}
	if err := json.Unmarshal(body, &displays); err != nil || displays == nil {
		return Map{}
	}
	return displays
}

func saveOrRemoveLocked(dir string, displays Map) error {
	if len(displays) != 0 {
		return saveLocked(dir, displays)
	}
	err := os.Remove(Path(dir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func saveLocked(dir string, displays Map) error {
	body, err := json.MarshalIndent(displays, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	destination := Path(dir)
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(destination); statErr == nil && info.Mode().IsRegular() {
		mode = info.Mode().Perm()
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}

	tmp, err := os.CreateTemp(dir, ".display.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(body); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	return fileutil.ReplaceFile(tmpPath, destination)
}

type directoryLock struct {
	mu   sync.Mutex
	refs int
}

var directoryLocks = struct {
	sync.Mutex
	entries map[string]*directoryLock
}{entries: map[string]*directoryLock{}}

func acquireDirectory(dir string) func() {
	key := canonicalDirectory(dir)
	directoryLocks.Lock()
	entry := directoryLocks.entries[key]
	if entry == nil {
		entry = &directoryLock{}
		directoryLocks.entries[key] = entry
	}
	entry.refs++
	directoryLocks.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		directoryLocks.Lock()
		entry.refs--
		if entry.refs == 0 && directoryLocks.entries[key] == entry {
			delete(directoryLocks.entries, key)
		}
		directoryLocks.Unlock()
	}
}

// canonicalDirectory also resolves a symlinked existing ancestor when dir has
// not been created yet. That makes aliases share a lock before the first Save.
func canonicalDirectory(dir string) string {
	canonical := filepath.Clean(dir)
	if absolute, err := filepath.Abs(canonical); err == nil {
		canonical = absolute
	}
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	} else {
		current := canonical
		missing := []string{}
		for {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			missing = append([]string{filepath.Base(current)}, missing...)
			current = parent
			if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
				parts := append([]string{resolved}, missing...)
				canonical = filepath.Join(parts...)
				break
			}
		}
	}
	canonical = filepath.Clean(canonical)
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	return canonical
}
